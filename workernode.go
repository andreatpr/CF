package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"runtime"
	"time"
)

var itemStats map[int]ItemStats
var movieData map[int]MovieData
var itemRatings ItemRatings

func computeSimilarity(aID, bID int) float64 {
	a, okA := itemStats[aID]
	b, okB := itemStats[bID]

	if !okA || !okB {
		return 0.0
	}

	simR := CosineSimilarityStats(a, b)

	var simG float64
	if mdA, ok := movieData[aID]; ok {
		if mdB, ok2 := movieData[bID]; ok2 {
			simG = GenreSimilarity(mdA.Genres, mdB.Genres)
		}
	}

	return 0.8*simR + 0.2*simG
}

func main() {
	port := flag.String("port", "9000", "port")
	flag.Parse()

	fmt.Println("Worker iniciando... Cargando datos...")

	var err error
	itemRatings, err = LoadItemRatingsMatrix("./analisisdata/resultados/20M/user_movie_matrix_20.csv")
	if err != nil {

		log.Fatalf("❌ WORKER ERROR: No se pudo cargar ratings: %v", err)
	}

	movieData, err = LoadMovieData("./analisisdata/resultados/20M/movies_clean_20.csv")
	if err != nil {
		log.Fatalf("❌ WORKER ERROR: No se pudo cargar movies: %v", err)
	}

	itemStats = PrecomputeItemStats(itemRatings)
	fmt.Printf("Datos cargados. %d peliculas en memoria.\n", len(itemStats))

	go func() {
		http.HandleFunc("/metrics", handleWorkerMetrics)
		fmt.Println("Servidor de métricas en puerto 9100")
		log.Println(http.ListenAndServe(":9100", nil))
	}()

	ln, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Worker escuchando en puerto", *port)

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		go handle(conn)

	}
}

func handle(conn net.Conn) {
	defer conn.Close()

	// Usamos Decoder/Encoder para manejar JSON directamente desde el socket
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var req BatchRequest

	// Leer el lote de tareas
	if err := decoder.Decode(&req); err != nil {
		log.Println("Error decodificando solicitud:", err)
		return
	}

	var resp BatchResponse

	// Procesar cada par del lote
	for _, p := range req.Pairs {
		sim := computeSimilarity(p.MovieA, p.MovieB)
		if sim > 0 { // Solo devolvemos si hay similitud relevante (optimización)
			resp.Results = append(resp.Results, CalculationResult{
				MovieA:     p.MovieA,
				MovieB:     p.MovieB,
				Similarity: sim,
			})
		}
	}

	// Enviar resultados de vuelta
	if err := encoder.Encode(resp); err != nil {
		log.Println("Error enviando respuesta:", err)
	}
}

func handleWorkerMetrics(w http.ResponseWriter, r *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	cpu := runtime.NumGoroutine() // Simple proxy (opcional)

	data := map[string]interface{}{
		"cpu":       cpu, // indicador
		"ram_mb":    memStats.Alloc / 1024 / 1024,
		"timestamp": time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
