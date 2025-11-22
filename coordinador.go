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

// ------------------------------
// VARIABLES GLOBALES
// ------------------------------
var (
	itemRatings ItemRatings
	movieData   map[int]MovieData
	itemStats   map[int]ItemStats
	allMovieIDs []int

	globalInv        map[int][]int
	globalCandidates map[int]map[int]int
	globalSimMatrix  map[int]map[int]float64

	// Variables para Docker y DB
	workerNodes []string
	rdb         *redis.Client
	mongoClient *mongo.Client
	ctx         = context.Background()
)

// ------------------------------
// WEBSOCKETS (Barra de progreso)
// ------------------------------
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}
var clients = make(map[*websocket.Conn]bool)
var broadcast = make(chan interface{})

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws.Close()
	clients[ws] = true

	for {
		var msg interface{}
		err := ws.ReadJSON(&msg)
		if err != nil {
			delete(clients, ws)
			break
		}
	}
}

func startBroadcaster() {
	for msg := range broadcast {
		for client := range clients {
			err := client.WriteJSON(msg)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
	}
}

// ------------------------------
// COMUNICACIÓN CON WORKERS (TCP - Lotes)
// ------------------------------
func RequestBatchSimilarity(workerAddr string, pairs []CalculationRequest) ([]CalculationResult, error) {
	conn, err := net.DialTimeout("tcp", workerAddr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// 1. Enviar Lote
	req := BatchRequest{Pairs: pairs}
	if err := encoder.Encode(req); err != nil {
		return nil, err
	}

	// 2. Recibir Resultados
	var resp BatchResponse
	if err := decoder.Decode(&resp); err != nil {
		return nil, err
	}

	return resp.Results, nil
}

// ------------------------------
// CÁLCULO DISTRIBUIDO
// ------------------------------
func ComputeDistributedSimilarityMatrixParallel(workers []string, candidates map[int]map[int]int) map[int]map[int]float64 {
	jobChan := make(chan []CalculationRequest, 100)
	resChan := make(chan []CalculationResult, 100)
	var wg sync.WaitGroup

	// --- Lanzar Workers ---
	for i := 0; i < len(workers)*2; i++ {
		workerAddr := workers[i%len(workers)]
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			for batch := range jobChan {
				res, err := RequestBatchSimilarity(addr, batch)
				if err == nil {
					resChan <- res
				} else {
					fmt.Println("Error en worker:", err)
				}
			}
		}(workerAddr)
	}

	// --- Productor de Tareas (Lotes) ---
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

	// --- Cerrar canal de resultados ---
	go func() {
		wg.Wait()
		close(resChan)
	}()

	// --- Recolector ---
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
				// Enviar a WebSocket
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

// ------------------------------
// RECOMENDACIONES & API
// ------------------------------
func ExtractUserRatings(r ItemRatings, userID int) map[int]float64 {
	out := make(map[int]float64)
	for movieID, v := range r {
		if rating, ok := v[userID]; ok {
			out[movieID] = rating
		}
	}
	return out
}

func generateDistributedRecommendations(userID int) ([]map[string]interface{}, error) {
	userRatings := ExtractUserRatings(itemRatings, userID)
	if len(userRatings) == 0 {
		return nil, fmt.Errorf("usuario sin ratings o no existe")
	}

	recs := GenerateRecommendations(userRatings, globalSimMatrix, allMovieIDs, 50)

	sort.Slice(recs, func(i, j int) bool {
		return recs[i].Score > recs[j].Score
	})

	out := []map[string]interface{}{}
	for i := 0; i < 10 && i < len(recs); i++ {
		out = append(out, map[string]interface{}{
			"movie_id": recs[i].MovieID,
			"title":    movieData[recs[i].MovieID].Title,
			"score":    recs[i].Score,
		})
	}
	return out, nil
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
		{Key: "endpoint", Value: "/recommend"},
	}
	// Usamos un contexto corto para no bloquear
	ctxT, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := coll.InsertOne(ctxT, doc)
	if err != nil {
		fmt.Println("Error guardando log en Mongo:", err)
	}
}

func handleRecommend(w http.ResponseWriter, r *http.Request) {
	uidStr := r.URL.Query().Get("user_id")
	uid, _ := strconv.Atoi(uidStr)
	if uid == 0 {
		http.Error(w, "user_id requerido", 400)
		return
	}

	// PASO A: Verificar Caché (Redis)
	val, err := rdb.Get(ctx, "recs:"+uidStr).Result()
	if err == nil {
		fmt.Printf("[CACHE HIT] Usuario %d recuperado de Redis\n", uid)
		go logRequestToMongo(uid, true)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(val))
		return
	}

	// PASO B: Calcular
	fmt.Printf("[CALCULANDO] Usuario %d procesando...\n", uid)
	recs, err := generateDistributedRecommendations(uid)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	jsonBytes, _ := json.Marshal(recs)

	// PASO C: Guardar en Caché y Mongo
	rdb.Set(ctx, "recs:"+uidStr, jsonBytes, 10*time.Minute)
	go logRequestToMongo(uid, false)

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonBytes)
}

// ------------------------------
// MAIN
// ------------------------------
func main() {
	// 1. Iniciar Bases de Datos (Función está en database.go)
	initDB()

	go startBroadcaster()

	workerEnv := os.Getenv("WORKERS")
	if workerEnv != "" {
		workerNodes = strings.Split(workerEnv, ",")
	} else {
		workerNodes = []string{"localhost:9001", "localhost:9002"}
	}
	fmt.Println("Workers configurados:", workerNodes)

	var err error
	fmt.Println("Cargando datos...")
	itemRatings, err = LoadItemRatingsMatrix("./analisisdata/resultados/20M/user_movie_matrix_20.csv")
	if err != nil {
		log.Fatal("Error cargando ratings:", err)
	}

	movieData, err = LoadMovieData("./analisisdata/resultados/20M/movies_clean_20.csv")
	if err != nil {
		log.Fatal("Error cargando movies:", err)
	}

	// 2. Migración (Función está en database.go)
	MigrateMoviesToMongo("./analisisdata/resultados/20M/movies_clean_20.csv")

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

	http.HandleFunc("/recommend", handleRecommend)
	http.HandleFunc("/ws", handleWebSocket)

	fmt.Println("✅ Coordinador listo en puerto :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
