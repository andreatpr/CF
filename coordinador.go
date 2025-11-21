package main

import (
	"bufio"
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
	"sync/atomic"
	"time"

	"github.com/go-redis/redis/v8"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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

	// Variables para el Entregable 2 (Docker y DB)
	workerNodes []string
	rdb         *redis.Client
	mongoClient *mongo.Client
	ctx         = context.Background()
)

// ------------------------------
// 1. INICIALIZACIÓN DE BASE DE DATOS (NUEVO)
// ------------------------------
func initDB() {
	fmt.Println("🔍 DEBUG: Verificando archivos en Docker...")
	files, errDebug := os.ReadDir("./analisisdata/resultados/20M")
	if errDebug != nil {
		fmt.Println("ERROR LEYENDO CARPETA 20M:", errDebug)
		// Intentar leer la carpeta padre para ver qué hay
		parent, _ := os.ReadDir("./analisisdata")
		fmt.Println("   Contenido de ./analisisdata:", parent)
	} else {
		fmt.Println("Archivos encontrados correctamente:")
		for _, f := range files {
			fmt.Println("   -", f.Name())
		}
	}
	// Conexión a Redis (Cache)
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379" // Valor por defecto si no estamos en Docker
	}
	rdb = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	// Conexión a MongoDB (Historial)
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017" // Valor por defecto
	}
	clientOptions := options.Client().ApplyURI(mongoURI)
	var err error
	mongoClient, err = mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatal("Error conectando a Mongo:", err)
	}

	fmt.Println("✔ Conectado a Redis y MongoDB exitosamente.")
}

// ------------------------------
// 2. COMUNICACIÓN CON WORKERS (TCP)
// ------------------------------
func RequestSimilarity(workers []string, a, b int) (float64, string, error) {
	for _, w := range workers {
		conn, err := net.DialTimeout("tcp", w, 2*time.Second)
		if err != nil {
			continue
		}

		fmt.Fprintf(conn, "SIMILARITY %d %d\n", a, b)

		// Leer respuesta
		resp, err := bufio.NewReader(conn).ReadString('\n')
		conn.Close()
		if err != nil {
			continue
		}

		parts := strings.Split(strings.TrimSpace(resp), " ")
		if len(parts) >= 2 && parts[0] == "RESULT" {
			v, _ := strconv.ParseFloat(parts[1], 64)
			return v, w, nil
		}
	}
	return 0, "", fmt.Errorf("ningún worker respondió")
}

// ------------------------------
// 3. CÁLCULO DISTRIBUIDO DE MATRIZ
// ------------------------------
func ComputeDistributedSimilarityMatrixParallel(
	workers []string,
	candidates map[int]map[int]int,
	numGoroutines int,
) map[int]map[int]float64 {

	type Pair struct{ A, B int }
	type Result struct {
		MovieA_ID  int
		MovieB_ID  int
		Similarity float64
	}

	jobChan := make(chan Pair, 1000)
	resChan := make(chan Result, 1000)

	var wg sync.WaitGroup

	// Contar total de pares para mostrar progreso
	totalPairs := int64(0)
	for _, row := range candidates {
		totalPairs += int64(len(row))
	}
	var processed int64 = 0
	var nextPrint int64 = 1

	fmt.Println("Total de pares a procesar:", totalPairs)

	// Lanzar Goroutines que consumen trabajos y llaman a Workers TCP
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pair := range jobChan {
				sim, _, err := RequestSimilarity(workers, pair.A, pair.B)
				if err == nil {
					resChan <- Result{MovieA_ID: pair.A, MovieB_ID: pair.B, Similarity: sim}
				}
			}
		}()
	}

	// Enviar trabajos (Fan-out)
	go func() {
		for a, row := range candidates {
			for b := range row {
				jobChan <- Pair{A: a, B: b}
			}
		}
		close(jobChan)
	}()

	// Cerrar canal de resultados cuando terminen las goroutines
	go func() {
		wg.Wait()
		close(resChan)
	}()

	// Recolectar resultados
	simMatrix := make(map[int]map[int]float64)

	for r := range resChan {
		if simMatrix[r.MovieA_ID] == nil {
			simMatrix[r.MovieA_ID] = make(map[int]float64)
		}
		if simMatrix[r.MovieB_ID] == nil {
			simMatrix[r.MovieB_ID] = make(map[int]float64)
		}
		simMatrix[r.MovieA_ID][r.MovieB_ID] = r.Similarity
		simMatrix[r.MovieB_ID][r.MovieA_ID] = r.Similarity

		// Mostrar progreso en consola
		done := atomic.AddInt64(&processed, 1)
		percent := (done * 100) / totalPairs
		if percent >= atomic.LoadInt64(&nextPrint) {
			fmt.Printf("   -> Avance matriz: %d%% (%d/%d)\r", percent, done, totalPairs)
			atomic.StoreInt64(&nextPrint, percent+1)
		}
	}

	fmt.Println("\n✔ SimMatrix completada al 100%")
	return simMatrix
}

// ------------------------------
// 4. GENERACIÓN DE RECOMENDACIONES
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

	// Llamada a función lógica en rec.go
	recs := GenerateRecommendations(userRatings, globalSimMatrix, allMovieIDs, 50)

	// Ordenar
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].Score > recs[j].Score
	})

	// Formato JSON
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

// ------------------------------
// 5. API HANDLER CON CACHÉ Y MONGO (NUEVO)
// ------------------------------
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
		// ¡Encontrado en caché!
		fmt.Printf("[CACHE HIT] Usuario %d recuperado de Redis\n", uid)

		// Log asíncrono en Mongo
		go logRequestToMongo(uid, true)

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(val))
		return
	}

	// PASO B: No está en caché, calcular
	fmt.Printf("[CALCULANDO] Usuario %d procesando...\n", uid)
	recs, err := generateDistributedRecommendations(uid)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	jsonBytes, _ := json.Marshal(recs)

	// PASO C: Guardar en Caché (Redis) por 10 minutos
	rdb.Set(ctx, "recs:"+uidStr, jsonBytes, 10*time.Minute)

	// PASO D: Guardar Historial (Mongo)
	go logRequestToMongo(uid, false)

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonBytes)
}

func logRequestToMongo(uid int, fromCache bool) {
	// Guardar log en base de datos 'recsys', colección 'history'
	coll := mongoClient.Database("recsys").Collection("history")
	doc := bson.D{
		{Key: "user_id", Value: uid},
		{Key: "timestamp", Value: time.Now()},
		{Key: "from_cache", Value: fromCache},
		{Key: "endpoint", Value: "/recommend"},
	}
	_, err := coll.InsertOne(ctx, doc)
	if err != nil {
		fmt.Println("Error guardando en Mongo:", err)
	}
}

// ------------------------------
// MAIN
// ------------------------------
func main() {
	// 1. Iniciar Bases de Datos
	initDB()

	// 2. Configurar Workers (Leyendo variable de entorno de Docker)
	workerEnv := os.Getenv("WORKERS")
	if workerEnv != "" {
		// Si estamos en Docker, usa los nombres definidos en docker-compose
		workerNodes = strings.Split(workerEnv, ",")
	} else {
		// Si corremos localmente sin Docker
		workerNodes = []string{"localhost:9001", "localhost:9002"}
	}
	fmt.Println("Workers configurados:", workerNodes)

	// 3. Cargar Datasets
	var err error
	fmt.Println("Cargando datos...")
	// Rutas relativas deben coincidir con el volumen de Docker
	itemRatings, err = LoadItemRatingsMatrix("./analisisdata/resultados/20M/user_movie_matrix_20.csv")
	if err != nil {
		log.Fatal("Error cargando ratings:", err)
	}

	movieData, err = LoadMovieData("./analisisdata/resultados/20M/movies_clean_20.csv")
	if err != nil {
		log.Fatal("Error cargando movies:", err)
	}

	// 4. Preprocesamiento
	itemStats = PrecomputeItemStats(itemRatings)
	fmt.Println("Construyendo índice invertido...")
	globalInv = BuildInvertedIndex(itemRatings, 50)

	fmt.Println("Generando candidatos...")
	globalCandidates = GenerateCandidatePairs(globalInv, 10)

	// 5. Cálculo Inicial de la Matriz (Distribuido)
	fmt.Println("Iniciando cálculo distribuido de la matriz de similitud...")
	globalSimMatrix = ComputeDistributedSimilarityMatrixParallel(workerNodes, globalCandidates, 200)

	// Preparar lista de IDs para recomendaciones
	for id := range itemRatings {
		allMovieIDs = append(allMovieIDs, id)
	}

	// 6. Iniciar Servidor
	http.HandleFunc("/recommend", handleRecommend)
	fmt.Println("Coordinador listo en puerto :8080")

	log.Fatal(http.ListenAndServe(":8080", nil))
}
