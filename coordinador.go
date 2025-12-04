package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	itemRatings ItemRatings
	movieData   map[int]MovieData
	itemStats   map[int]ItemStats
	allMovieIDs []int

	globalInv        map[int][]int
	globalCandidates map[int]map[int]int
	globalSimMatrix  map[int]map[int]float64

	workerNodes []string
	rdb         *redis.Client
	mongoClient *mongo.Client
	ctx         = context.Background()
)
var uniqueGenres []string

// --- Función Auxiliar (Ponla antes del main o por donde están las otras funcs) ---
func ExtractUniqueGenres(data map[int]MovieData) []string {
	genreSet := make(map[string]bool)

	// 1. Recorrer todas las películas en memoria
	for _, movie := range data {
		for _, g := range movie.Genres {
			g = strings.TrimSpace(g)
			if g != "" && g != "(no genres listed)" {
				genreSet[g] = true
			}
		}
	}

	// 2. Convertir el Mapa (Set) a Slice (Lista)
	var list []string
	for g := range genreSet {
		list = append(list, g)
	}

	// 3. Ordenar alfabéticamente para que se vea bonito en el select
	sort.Strings(list)
	return list
}

// --- Handler del Endpoint ---
func handleGetGenres(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(uniqueGenres)
}

// --- WebSockets ---
var upgrader = websocket.Upgrader{
	// IMPORTANTE: Permitir conexión desde cualquier origen (React)
	CheckOrigin: func(r *http.Request) bool { return true },
}
var clients = make(map[*websocket.Conn]bool)
var broadcast = make(chan interface{})

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	clients[ws] = true
	for {
		var msg interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			delete(clients, ws)
			break
		}
	}
}

func startBroadcaster() {
	for msg := range broadcast {
		for client := range clients {
			client.WriteJSON(msg)
		}
	}
}

// --- Cliente TCP Lotes ---
func RequestBatchSimilarity(workerAddr string, pairs []CalculationRequest) ([]CalculationResult, error) {
	conn, err := net.DialTimeout("tcp", workerAddr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(BatchRequest{Pairs: pairs}); err != nil {
		return nil, err
	}
	var resp BatchResponse
	if err := decoder.Decode(&resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// --- Dispatcher ---
func ComputeDistributedSimilarityMatrixParallel(workers []string, candidates map[int]map[int]int) map[int]map[int]float64 {
	jobChan := make(chan []CalculationRequest, 100)
	resChan := make(chan []CalculationResult, 100)
	var wg sync.WaitGroup

	for i := 0; i < len(workers)*2; i++ {
		workerAddr := workers[i%len(workers)]
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			for batch := range jobChan {
				if res, err := RequestBatchSimilarity(addr, batch); err == nil {
					resChan <- res
				}
			}
		}(workerAddr)
	}

	go func() {
		batchSize := 2000
		currentBatch := []CalculationRequest{}
		for a, row := range candidates {
			for b := range row {
				currentBatch = append(currentBatch, CalculationRequest{MovieA: a, MovieB: b})
				if len(currentBatch) >= batchSize {
					jobChan <- currentBatch
					currentBatch = []CalculationRequest{}
				}
			}
		}
		if len(currentBatch) > 0 {
			jobChan <- currentBatch
		}
		close(jobChan)
	}()

	go func() { wg.Wait(); close(resChan) }()

	simMatrix := make(map[int]map[int]float64)
	totalPairs := 0
	for _, r := range candidates {
		totalPairs += len(r)
	}
	processed := 0
	lastPorcent := -1

	for batchRes := range resChan {
		for _, res := range batchRes {
			if simMatrix[res.MovieA] == nil {
				simMatrix[res.MovieA] = make(map[int]float64)
			}
			if simMatrix[res.MovieB] == nil {
				simMatrix[res.MovieB] = make(map[int]float64)
			}
			simMatrix[res.MovieA][res.MovieB] = res.Similarity
			simMatrix[res.MovieB][res.MovieA] = res.Similarity
		}
		processed += len(batchRes)
		if totalPairs > 0 {
			porcent := int((float64(processed) / float64(totalPairs)) * 100)
			if porcent > lastPorcent {
				fmt.Printf("Progreso: %d%%\r", porcent)
				select {
				case broadcast <- map[string]interface{}{"type": "progress", "value": porcent}:
				default:
				}
				lastPorcent = porcent
			}
		}
	}
	return simMatrix
}

// --- API & CORS ---

// NUEVO: Middleware para permitir CORS (Permite que React se conecte)
func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Permitir acceso desde cualquier origen
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Si es una solicitud de verificación (OPTIONS), respondemos OK y salimos
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

func handleRecommend(w http.ResponseWriter, r *http.Request) {
	uidStr := r.URL.Query().Get("user_id")
	uid, _ := strconv.Atoi(uidStr)

	// 1. Leer nuevos parámetros
	genre := r.URL.Query().Get("genre") // Ej: "Adventure" o ""
	if genre == "" {
		genre = "All"
	}

	limitStr := r.URL.Query().Get("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 10
	} // Default 10

	// 2. Clave de Caché COMPUESTA (Para diferenciar búsquedas)
	// Ej: "recs:101:Adventure:15"
	cacheKey := fmt.Sprintf("recs:%d:%s:%d", uid, genre, limit)

	// Check Cache
	if val, err := rdb.Get(ctx, cacheKey).Result(); err == nil {
		fmt.Printf("[CACHE HIT] Usuario %d Género %s\n", uid, genre)
		go logRequestToMongo(uid, true) // Podrías guardar el género en mongo también si quieres
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(val))
		return
	}

	// Calculate (Pasamos los nuevos parámetros)
	recs, err := generateDistributedRecommendations(uid, genre, limit)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	jsonBytes, _ := json.Marshal(recs)
	rdb.Set(ctx, cacheKey, jsonBytes, 10*time.Minute)
	go logRequestToMongo(uid, false)

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonBytes)
}

func logRequestToMongo(uid int, fromCache bool) {
	if mongoClient == nil {
		return
	}
	coll := mongoClient.Database("recsys").Collection("history")
	doc := bson.D{
		{Key: "user_id", Value: uid},
		{Key: "timestamp", Value: time.Now()},
		{Key: "from_cache", Value: fromCache},
	}
	ctxT, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	coll.InsertOne(ctxT, doc)
}

// Actualizar la llamada a la función de lógica
func generateDistributedRecommendations(userID int, genre string, limit int) ([]map[string]interface{}, error) {
	userRatings := ExtractUserRatings(itemRatings, userID)
	if len(userRatings) == 0 {
		return nil, fmt.Errorf("usuario sin ratings")
	}

	// Pasamos movieData, genre y limit a la función que editamos en rec.go
	recs := GenerateRecommendations(userRatings, globalSimMatrix, allMovieIDs, movieData, genre, limit)

	// (El sort ya se hizo dentro de GenerateRecommendations, pero no hace daño)

	out := []map[string]interface{}{}
	for _, r := range recs {
		out = append(out, map[string]interface{}{
			"movie_id": r.MovieID,
			"title":    movieData[r.MovieID].Title,
			"score":    r.Score,
			"genres":   movieData[r.MovieID].Genres, // Opcional: devolver géneros al front
		})
	}
	return out, nil
}

func ExtractUserRatings(r ItemRatings, userID int) map[int]float64 {
	out := make(map[int]float64)
	for movieID, v := range r {
		if rating, ok := v[userID]; ok {
			out[movieID] = rating
		}
	}
	return out
}

func main() {
	initDB() // De database.go
	go startBroadcaster()

	workerEnv := os.Getenv("WORKERS")
	if workerEnv != "" {
		workerNodes = strings.Split(workerEnv, ",")
	} else {
		workerNodes = []string{"localhost:9001", "localhost:9002"}
	}
	fmt.Println("Workers configurados:", workerNodes)

	// RUTAS DE ARCHIVOS (Ajustadas a tu estructura actual)
	// Si tus archivos están sueltos en analisisdata, usa estas rutas:
	const ratingsPath = "./analisisdata/resultados/20M/user_movie_matrix_20.csv"
	const moviesPath = "./analisisdata/resultados/20M/movies_clean_20.csv"

	var err error
	fmt.Println("Cargando datos...")
	itemRatings, err = LoadItemRatingsMatrix(ratingsPath)
	if err != nil {
		log.Fatal("Error cargando ratings:", err)
	}

	movieData, err = LoadMovieData(moviesPath)
	if err != nil {
		log.Fatal("Error cargando movies:", err)
	}
	// AGREGA ESTO INMEDIATAMENTE DESPUÉS DE CARGAR MOVIEDATA:
	fmt.Println("Extrayendo géneros únicos...")
	uniqueGenres = ExtractUniqueGenres(movieData)
	fmt.Printf("Se encontraron %d géneros únicos.\n", len(uniqueGenres))

	// Migrar usando la ruta simplificada
	MigrateMoviesToMongo(moviesPath)

	itemStats = PrecomputeItemStats(itemRatings)
	fmt.Println("Construyendo índice invertido...")
	globalInv = BuildInvertedIndex(itemRatings, 50)

	fmt.Println("Generando candidatos...")
	globalCandidates = GenerateCandidatePairs(globalInv, 10)

	fmt.Println("Iniciando cálculo distribuido...")
	globalSimMatrix = ComputeDistributedSimilarityMatrixParallel(workerNodes, globalCandidates)

	for id := range itemRatings {
		allMovieIDs = append(allMovieIDs, id)
	}

	// --- AQUI ESTA LA MAGIA DEL CORS ---
	// Envolvemos el handler con enableCORS
	http.HandleFunc("/recommend", enableCORS(handleRecommend))

	http.HandleFunc("/ws", handleWebSocket)
	http.HandleFunc("/genres", enableCORS(handleGetGenres))

	fmt.Println("✅ Coordinador listo en puerto :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
