package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ------------------ Estructuras ------------------

type ItemRatings map[int]map[int]float64

type Recommendation struct {
	MovieID int
	Score   float64
}

type Job struct {
	MovieA_ID int
	MovieB_ID int
	StatsA    ItemStats
	StatsB    ItemStats
}

type Result struct {
	MovieA_ID  int
	MovieB_ID  int
	Similarity float64
}

type ItemStats struct {
	Norm    float64
	Mean    float64
	Ratings map[int]float64
	UserSet map[int]struct{}
}

type MovieData struct {
	Title  string
	Genres []string
}

// ------------------ Variables globales ------------------

var similarityAlgorithm = "cosine" // "cosine", "pearson", "jaccard"

// ------------------ Funciones de preprocesamiento ------------------

func PrecomputeItemStats(ratings ItemRatings) map[int]ItemStats {
	stats := make(map[int]ItemStats, len(ratings))
	for movieID, vec := range ratings {
		sum, sumSq := 0.0, 0.0
		userSet := make(map[int]struct{}, len(vec))
		for _, rating := range vec {
			sum += rating
			sumSq += rating * rating
		}
		mean := sum / float64(len(vec))
		norm := math.Sqrt(sumSq)
		for userID := range vec {
			userSet[userID] = struct{}{}
		}
		stats[movieID] = ItemStats{
			Norm:    norm,
			Mean:    mean,
			Ratings: vec,
			UserSet: userSet,
		}
	}
	return stats
}

// ------------------ Funciones de similitud ------------------

func CosineSimilarityStats(a, b ItemStats) float64 {
	dot := 0.0
	for userID, valA := range a.Ratings {
		if valB, ok := b.Ratings[userID]; ok {
			dot += valA * valB
		}
	}
	if a.Norm == 0 || b.Norm == 0 {
		return 0.0
	}
	return dot / (a.Norm * b.Norm)
}

func PearsonCorrelationStats(a, b ItemStats) float64 {
	commonKeys := []int{}
	for userID := range a.Ratings {
		if _, ok := b.Ratings[userID]; ok {
			commonKeys = append(commonKeys, userID)
		}
	}
	if len(commonKeys) == 0 {
		return 0.0
	}
	num, sumSqA, sumSqB := 0.0, 0.0, 0.0
	for _, userID := range commonKeys {
		ca := a.Ratings[userID] - a.Mean
		cb := b.Ratings[userID] - b.Mean
		num += ca * cb
		sumSqA += ca * ca
		sumSqB += cb * cb
	}
	den := math.Sqrt(sumSqA) * math.Sqrt(sumSqB)
	if den == 0 {
		return 0.0
	}
	return num / den
}

func JaccardIndexStats(a, b ItemStats) float64 {
	intersection := 0.0
	unionSet := make(map[int]struct{})
	for userID := range a.UserSet {
		unionSet[userID] = struct{}{}
		if _, ok := b.UserSet[userID]; ok {
			intersection++
		}
	}
	for userID := range b.UserSet {
		unionSet[userID] = struct{}{}
	}
	if len(unionSet) == 0 {
		return 0.0
	}
	return intersection / float64(len(unionSet))
}

// ------------------ Carga de datos ------------------

func LoadItemRatingsMatrix(path string) (ItemRatings, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	header, _ := reader.Read() // primera fila: "user_id", movieIDs...

	movieIDs := []int{}
	for _, h := range header[1:] {
		id, _ := strconv.Atoi(h)
		movieIDs = append(movieIDs, id)
	}

	itemRatings := make(ItemRatings)
	for i := 0; i < len(movieIDs); i++ {
		itemRatings[movieIDs[i]] = make(map[int]float64)
	}

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		userID, _ := strconv.Atoi(record[0])
		for j, val := range record[1:] {
			if val == "" {
				continue
			}
			r, _ := strconv.ParseFloat(val, 64)
			itemRatings[movieIDs[j]][userID] = r
		}
	}

	fmt.Printf("Cargados ratings para %d películas.\n", len(itemRatings))
	return itemRatings, nil
}

func LoadMovieData(path string) (map[int]MovieData, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error al abrir el archivo de películas: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Read() // omitir encabezado

	movies := make(map[int]MovieData)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}

		movieID, _ := strconv.Atoi(record[0])
		title := record[1]
		genres := strings.Split(record[2], "|")

		movies[movieID] = MovieData{
			Title:  title,
			Genres: genres,
		}
	}
	fmt.Printf("Cargados %d títulos de películas con géneros.\n", len(movies))
	return movies, nil
}

func BuildInvertedIndex(items ItemRatings, maxMoviesPerUser int) map[int][]int {
	inv := make(map[int][]int)
	userMovieCount := make(map[int]int)
	totalMovies := len(items)
	processed := 0
	step := totalMovies / 100
	if step == 0 {
		step = 1
	}

	// primero contar cuántas películas calificó cada usuario
	for _, umap := range items {
		for uid := range umap {
			userMovieCount[uid]++
		}
	}

	// construir solo para usuarios *útiles*
	for movieID, umap := range items {
		for uid := range umap {
			if userMovieCount[uid] <= maxMoviesPerUser { // ← filtro clave
				inv[uid] = append(inv[uid], movieID)
			}
		}
		processed++
		if processed%step == 0 {
			p := float64(processed) * 100 / float64(totalMovies)
			fmt.Printf("Progreso Inverted Index: %.2f%%\r", p)
		}
	}

	for uid := range inv {
		sort.Ints(inv[uid])
	}
	return inv
}

// ------------------ Similitud de géneros ------------------

func GenreSimilarity(genresA, genresB []string) float64 {
	setA := make(map[string]struct{})
	setB := make(map[string]struct{})
	for _, g := range genresA {
		setA[strings.TrimSpace(g)] = struct{}{}
	}
	for _, g := range genresB {
		setB[strings.TrimSpace(g)] = struct{}{}
	}
	inter := 0
	for g := range setA {
		if _, ok := setB[g]; ok {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func GenerateCandidatePairs(inv map[int][]int, minCo int) map[int]map[int]int {
	cand := make(map[int]map[int]int)

	// Total de usuarios (para progreso)
	totalUsers := len(inv)
	processed := 0
	step := totalUsers / 100
	if step == 0 {
		step = 1
	}

	for _, movies := range inv {
		for a := 0; a < len(movies); a++ {
			i := movies[a]
			for b := a + 1; b < len(movies); b++ {
				j := movies[b]
				if i > j {
					i, j = j, i
				}
				if cand[i] == nil {
					cand[i] = make(map[int]int)
				}
				cand[i][j]++
			}
		}

		processed++
		if processed%step == 0 {
			p := float64(processed) * 100 / float64(totalUsers)
			fmt.Printf("Progreso Candidate Pairs: %.2f%%\r", p)
		}
	}
	fmt.Println("\nGeneración de pares candidatos completada.")

	if minCo <= 1 {
		return cand
	}

	// Filtrado por mínimo co-rating
	for i, row := range cand {
		for j, c := range row {
			if c < minCo {
				delete(row, j)
			}
		}
		if len(row) == 0 {
			delete(cand, i)
		}
	}
	fmt.Println("Filtrado por co-rating mínimo completado.")
	return cand
}

// ------------------ Baseline (secuencial, p=1) ------------------
func CalculateItemSimilaritiesStats_Sequential(
	stats map[int]ItemStats,
	movieData map[int]MovieData, candidates map[int]map[int]int,
	alpha float64,
) map[int]map[int]float64 {

	// contar trabajos
	numJobs := 0
	for i := range candidates {
		numJobs += len(candidates[i])
	}

	simMatrix := make(map[int]map[int]float64)

	// progreso
	done := 0
	step := numJobs / 100
	if step == 0 {
		step = 1
	}

	for i, row := range candidates {
		for j := range row {
			done++
			if done%step == 0 {
				fmt.Printf("Progreso (secuencial): %.2f%%\r", float64(done)*100/float64(numJobs))
			}

			a := stats[i]
			b := stats[j]
			var simRatings float64
			switch similarityAlgorithm {
			case "pearson":
				simRatings = PearsonCorrelationStats(a, b)
			case "jaccard":
				simRatings = JaccardIndexStats(a, b)
			default:
				simRatings = CosineSimilarityStats(a, b)
			}
			simGenres := GenreSimilarity(movieData[i].Genres, movieData[j].Genres)
			sim := alpha*simRatings + (1-alpha)*simGenres

			if sim > 0.1 {
				if simMatrix[i] == nil {
					simMatrix[i] = make(map[int]float64)
				}
				if simMatrix[j] == nil {
					simMatrix[j] = make(map[int]float64)
				}
				simMatrix[i][j] = sim
				simMatrix[j][i] = sim
			}
		}
	}
	fmt.Println("\nCálculo secuencial completado.")
	return simMatrix
}

// ------------------ Worker concurrente ------------------

func WorkerStats(jobs <-chan Job, results chan<- Result, progress chan<- struct{}, movieData map[int]MovieData, alpha float64) {
	for job := range jobs {
		var simRatings float64
		switch similarityAlgorithm {
		case "pearson":
			simRatings = PearsonCorrelationStats(job.StatsA, job.StatsB)
		case "jaccard":
			simRatings = JaccardIndexStats(job.StatsA, job.StatsB)
		default:
			simRatings = CosineSimilarityStats(job.StatsA, job.StatsB)
		}
		genresA := movieData[job.MovieA_ID].Genres
		genresB := movieData[job.MovieB_ID].Genres
		simGenres := GenreSimilarity(genresA, genresB)
		sim := alpha*simRatings + (1-alpha)*simGenres

		// Marca de progreso (cuenta pares procesados)
		progress <- struct{}{}

		if sim > 0.1 {
			results <- Result{
				MovieA_ID:  job.MovieA_ID,
				MovieB_ID:  job.MovieB_ID,
				Similarity: sim,
			}
		}
	}
}

func CalculateItemSimilaritiesStats_Parallel(
	stats map[int]ItemStats,
	movieData map[int]MovieData,
	candidates map[int]map[int]int, // i -> j -> co-count
	numWorkers int, alpha float64,
) map[int]map[int]float64 {

	jobs := make(chan Job, 1000)
	results := make(chan Result, 1000)
	progress := make(chan struct{}, 1000)
	var wg sync.WaitGroup

	// cuenta de trabajos reales
	numJobs := 0
	for i := range candidates {
		numJobs += len(candidates[i])
	}

	// workers
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			WorkerStats(jobs, results, progress, movieData, alpha)
		}()
	}

	// fan-out SOLO de candidatos
	go func() {
		for i, row := range candidates {
			for j := range row {
				jobs <- Job{
					MovieA_ID: i,
					MovieB_ID: j,
					StatsA:    stats[i],
					StatsB:    stats[j],
				}
			}
		}
		close(jobs)
	}()

	// cierre ordenado
	go func() { wg.Wait(); close(results); close(progress) }()

	// progreso
	go func() {
		counter := 0
		step := numJobs / 100
		if step == 0 {
			step = 1
		}
		for range progress {
			counter++
			if counter%step == 0 {
				fmt.Printf("Progreso: %.2f%%\r", float64(counter)*100/float64(numJobs))
			}
		}
	}()

	// recolector
	simMatrix := make(map[int]map[int]float64)
	for r := range results {
		if simMatrix[r.MovieA_ID] == nil {
			simMatrix[r.MovieA_ID] = make(map[int]float64)
		}
		if simMatrix[r.MovieB_ID] == nil {
			simMatrix[r.MovieB_ID] = make(map[int]float64)
		}
		simMatrix[r.MovieA_ID][r.MovieB_ID] = r.Similarity
		simMatrix[r.MovieB_ID][r.MovieA_ID] = r.Similarity
	}
	fmt.Println("\nCálculo de similitudes completado.")
	return simMatrix
}

// ------------------ Recomendaciones ------------------

func GenerateRecommendations(userRatings map[int]float64, simMatrix map[int]map[int]float64, allMovieIDs []int, movieData map[int]MovieData, targetGenre string, limit int) []Recommendation {
	recommendations := []Recommendation{}

	// Configuración de vecinos para el algoritmo (fijo internamente, ej: 25 vecinos)
	neighborK := 25

	for _, movieID := range allMovieIDs {
		// 1. Si el usuario ya la vio, saltar
		if _, seen := userRatings[movieID]; seen {
			continue
		}

		// 2. NUEVO: Filtrado por género
		// Si targetGenre no es vacío y "All", verificamos si la peli tiene ese género
		if targetGenre != "" && targetGenre != "All" {
			mData, exists := movieData[movieID]
			if !exists || !containsGenre(mData.Genres, targetGenre) {
				continue // Si no es del género, la ignoramos antes de calcular
			}
		}

		// 3. Predecir score
		predictedScore := PredictScore(movieID, userRatings, simMatrix, neighborK)
		if predictedScore > 0 {
			recommendations = append(recommendations, Recommendation{MovieID: movieID, Score: predictedScore})
		}
	}

	// Ordenar por score
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Score > recommendations[j].Score
	})

	// 4. NUEVO: Respetar el límite solicitado (5, 10, 15...)
	if len(recommendations) > limit {
		return recommendations[:limit]
	}
	return recommendations
}

// Función auxiliar pequeña para buscar en el slice de strings
func containsGenre(genres []string, target string) bool {
	for _, g := range genres {
		if strings.TrimSpace(g) == target {
			return true
		}
	}
	return false
}

func PredictScore(movieID int, userRatings map[int]float64, simMatrix map[int]map[int]float64, k int) float64 {
	numerator, denominator := 0.0, 0.0
	movieSimilarities, ok := simMatrix[movieID]
	if !ok {
		return 0.0
	}
	neighbors := []Recommendation{}
	for otherMovieID, similarity := range movieSimilarities {
		if _, rated := userRatings[otherMovieID]; rated {
			// (opcional) ignorar similitudes negativas:
			// if similarity <= 0 { continue }
			neighbors = append(neighbors, Recommendation{MovieID: otherMovieID, Score: similarity})
		}
	}
	sort.Slice(neighbors, func(i, j int) bool {
		return neighbors[i].Score > neighbors[j].Score
	})
	if len(neighbors) > k {
		neighbors = neighbors[:k]
	}
	for _, neighbor := range neighbors {
		numerator += neighbor.Score * userRatings[neighbor.MovieID]
		denominator += neighbor.Score
	}
	if denominator == 0 {
		return 0.0
	}
	return numerator / denominator
}

// ------------------ MAIN ------------------
/*
func main() {
	fmt.Println("--- Recomendaciones usando matriz usuario-película + géneros ---")
	runtime.GOMAXPROCS(runtime.NumCPU()) // asegurar que se usen todos los CPUs

	matrixPath := "./25M/25M/user_movie_matrix.csv"
	moviePath := "./25M/25M/movies_clean.csv"
	algorithms := []string{"cosine", "pearson", "jaccard"}

	// Puedes ajustar los workers que quieres medir; agregamos 1 para baseline paralela si quieres comparar apples-to-apples.
	userWorkers := []int{6, 8, 12, 16}

	targetUser := 100
	k := 25
	alpha := 0.8 // peso para ratings vs géneros

	// Cargar una sola vez por ejecución de algoritmo (evita recomputar/recargar)
	itemRatings, err := loadItemRatingsMatrix(matrixPath)
	if err != nil {
		log.Fatal(err)
	}
	movieData, err := loadMovieData(moviePath)
	if err != nil {
		log.Fatal(err)
	}
	itemStats := precomputeItemStats(itemRatings)

	// Para reportar
	type row struct {
		Algo    string
		P       int
		Time    time.Duration
		Speedup float64
		Eff     float64
		Pairs   int
	}
	report := []row{}

	inv := buildInvertedIndex(itemRatings, 100)
	minCo := 6 // exigir al menos 6 usuarios en común
	candidates := generateCandidatePairs(inv, minCo)

	for _, algo := range algorithms {
		similarityAlgorithm = algo
		fmt.Printf("\n=== Algoritmo: %s ===\n", algo)

		t0 := time.Now()
		_ = calculateItemSimilaritiesStats_Sequential(itemStats, movieData, candidates, alpha)
		T1 := time.Since(t0)
		fmt.Printf("Baseline secuencial (co-rating) T1 = %v (pares candidatos=%d)\n",
			T1, countPairs(candidates))

		// paralelo con candidatos
		for _, p := range userWorkers {
			start := time.Now()
			simMatrix := calculateItemSimilaritiesStats_Parallel(itemStats, movieData, candidates, p, alpha)
			Tp := time.Since(start)
			speedup := float64(T1) / float64(Tp)
			eff := speedup / float64(p)
			fmt.Printf("p=%-2d  Tp=%-12v  Speedup=%5.2f  Efficiency=%5.2f  (pares=%d)\n",
				p, Tp, speedup, eff, countPairs(candidates))

			report = append(report, row{
				Algo: similarityAlgorithm, P: p, Time: Tp,
				Speedup: speedup, Eff: eff, Pairs: countPairs(candidates),
			})

			// Generar recomendaciones para este valor de p
			userRatings := make(map[int]float64)
			for movieID, ratings := range itemRatings {
				if rating, ok := ratings[targetUser]; ok {
					userRatings[movieID] = rating
				}
			}

			allMovieIDs := make([]int, 0, len(itemRatings))
			for id := range itemRatings {
				allMovieIDs = append(allMovieIDs, id)
			}

			recs := generateRecommendations(userRatings, simMatrix, allMovieIDs, k)

			fmt.Printf("\n--- Top 10 Recomendaciones (p=%d) ---\n", p)
			for i, rec := range recs {
				if i >= 10 {
					break
				}
				title := movieData[rec.MovieID].Title
				fmt.Printf("%d. %s (ID: %d) - Puntaje Previsto: %.4f\n",
					i+1, title, rec.MovieID, rec.Score)
			}
			fmt.Println()

		}

	}

	// tabla compacta final
	fmt.Println("\n=== Resumen Speedup / Efficiency ===")
	fmt.Println("Algo   p   Time(ms)   Speedup   Efficiency")
	for _, r := range report {
		fmt.Printf("%-6s %-2d  %-9.3f %-8.2f %-10.2f\n",
			r.Algo, r.P, float64(r.Time.Microseconds())/1000.0, r.Speedup, r.Eff)
	}
}
*/
func countPairs(candidates map[int]map[int]int) int {
	total := 0
	for _, row := range candidates {
		total += len(row)
	}
	return total
}

// --- Estructuras para Comunicación por Lotes (NUEVO) ---

type CalculationRequest struct {
	MovieA int
	MovieB int
}

type BatchRequest struct {
	Pairs []CalculationRequest
}

type CalculationResult struct {
	MovieA     int
	MovieB     int
	Similarity float64
}

type BatchResponse struct {
	Results []CalculationResult
}
