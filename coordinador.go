package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	itemRatings ItemRatings
	movieData   map[int]MovieData
	itemStats   map[int]ItemStats
	allMovieIDs []int

	globalInv        map[int][]int
	globalCandidates map[int]map[int]int
	globalSimMatrix  map[int]map[int]float64
)

var workerNodes = []string{
	"localhost:9001",
	"localhost:9002",
}

// ------------------------------
// WORKER PING
// ------------------------------
func RequestSimilarity(workers []string, a, b int) (float64, string, error) {
	for _, w := range workers {
		conn, err := net.Dial("tcp", w)
		if err != nil {
			continue
		}
		defer conn.Close()

		fmt.Fprintf(conn, "SIMILARITY %d %d\n", a, b)

		resp, _ := bufio.NewReader(conn).ReadString('\n')
		parts := strings.Split(strings.TrimSpace(resp), " ")

		if len(parts) == 2 && parts[0] == "RESULT" {
			v, _ := strconv.ParseFloat(parts[1], 64)
			return v, w, nil
		}
	}

	return 0, "", fmt.Errorf("ningún worker respondió")
}

// ------------------------------
// EXTRACT USER RATINGS
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

// ------------------------------
// DISTRIBUTED SIMILARITY MATRIX
// ------------------------------
func ComputeDistributedSimilarityMatrixParallel(
	workers []string,
	candidates map[int]map[int]int,
	numGoroutines int,
) map[int]map[int]float64 {

	type Pair struct{ A, B int }

	jobChan := make(chan Pair, 1000)
	resChan := make(chan Result, 1000)

	var wg sync.WaitGroup

	totalPairs := int64(countPairs(candidates))
	var processed int64 = 0
	var nextPrint int64 = 1

	fmt.Println("Total de pares a procesar:", totalPairs)

	// Launch goroutines
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

	// Fan-out
	go func() {
		for a, row := range candidates {
			for b := range row {
				jobChan <- Pair{A: a, B: b}
			}
		}
		close(jobChan)
	}()

	// Close resChan after workers finish
	go func() {
		wg.Wait()
		close(resChan)
	}()

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

		// Progreso
		done := atomic.AddInt64(&processed, 1)
		percent := (done * 100) / totalPairs
		if percent >= atomic.LoadInt64(&nextPrint) {
			fmt.Printf("   -> Avance matriz: %d%% (%d/%d)\n", percent, done, totalPairs)
			atomic.StoreInt64(&nextPrint, percent+1)
		}
	}

	fmt.Println("✔ SimMatrix completada al 100%")
	return simMatrix
}

// ------------------------------
// GENERATE RECOMMENDATIONS
// ------------------------------
func generateDistributedRecommendations(userID int) ([]map[string]interface{}, error) {

	userRatings := ExtractUserRatings(itemRatings, userID)
	if len(userRatings) == 0 {
		return nil, fmt.Errorf("usuario sin ratings")
	}

	fmt.Println("UserRatings:", len(userRatings))
	fmt.Println("SimMatrix size:", len(globalSimMatrix))

	recs := GenerateRecommendations(userRatings, globalSimMatrix, allMovieIDs, 50)

	fmt.Println("Total recomendaciones generadas:", len(recs))

	// DEBUG raw top-10
	fmt.Println("Top 10 raw recommendations:")
	for i := 0; i < 10 && i < len(recs); i++ {
		fmt.Printf("%2d) %-40s  Score=%.4f\n",
			recs[i].MovieID,
			movieData[recs[i].MovieID].Title,
			recs[i].Score,
		)
	}

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

// ------------------------------
// MAIN
// ------------------------------
func main() {
	var err error

	itemRatings, err = LoadItemRatingsMatrix("./analisisdata/resultados/20M/user_movie_matrix_20.csv")
	if err != nil {
		log.Fatal(err)
	}

	movieData, err = LoadMovieData("./analisisdata/resultados/20M/movies_clean_20.csv")
	if err != nil {
		log.Fatal(err)
	}

	itemStats = PrecomputeItemStats(itemRatings)

	fmt.Println("Construyendo índice invertido...")
	globalInv = BuildInvertedIndex(itemRatings, 50)

	fmt.Println("Generando candidatos (co-rating >=6)...")
	globalCandidates = GenerateCandidatePairs(globalInv, 10)

	fmt.Println("Calculando matriz de similitud distribuida...")
	globalSimMatrix = ComputeDistributedSimilarityMatrixParallel(workerNodes, globalCandidates, 200)

	// colectar movieIDs
	for id := range itemRatings {
		allMovieIDs = append(allMovieIDs, id)
	}

	fmt.Println("✔ Matriz distribuida construida.")

	http.HandleFunc("/recommend", handleRecommend)
	fmt.Println("Coordinador listo en :8080")

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleRecommend(w http.ResponseWriter, r *http.Request) {
	uid, _ := strconv.Atoi(r.URL.Query().Get("user_id"))
	if uid == 0 {
		http.Error(w, "user_id requerido", 400)
		return
	}

	recs, err := generateDistributedRecommendations(uid)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	json.NewEncoder(w).Encode(recs)
}
