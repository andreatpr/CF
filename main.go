package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
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

func precomputeItemStats(ratings ItemRatings) map[int]ItemStats {
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

func cosineSimilarityStats(a, b ItemStats) float64 {
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

func pearsonCorrelationStats(a, b ItemStats) float64 {
	commonKeys := []int{}
	for userID := range a.Ratings {
		if _, ok := b.Ratings[userID]; ok {
			commonKeys = append(commonKeys, userID)
		}
	}
	n := float64(len(commonKeys))
	if n == 0 {
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

func jaccardIndexStats(a, b ItemStats) float64 {
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

func loadItemRatingsMatrix(path string) (ItemRatings, error) {
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

func loadMovieData(path string) (map[int]MovieData, error) {
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

// ------------------ Similitud de géneros ------------------

func genreSimilarity(genresA, genresB []string) float64 {
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

// ------------------ Worker concurrente ------------------

func workerStats(jobs <-chan Job, results chan<- Result, progress chan<- struct{}, movieData map[int]MovieData, alpha float64) {
	for job := range jobs {
		var simRatings float64
		switch similarityAlgorithm {
		case "pearson":
			simRatings = pearsonCorrelationStats(job.StatsA, job.StatsB)
		case "jaccard":
			simRatings = jaccardIndexStats(job.StatsA, job.StatsB)
		default:
			simRatings = cosineSimilarityStats(job.StatsA, job.StatsB)
		}

		genresA := movieData[job.MovieA_ID].Genres
		genresB := movieData[job.MovieB_ID].Genres
		simGenres := genreSimilarity(genresA, genresB)

		sim := alpha*simRatings + (1-alpha)*simGenres
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

func calculateItemSimilaritiesStats_Parallel(stats map[int]ItemStats, movieData map[int]MovieData, numWorkers int, alpha float64) map[int]map[int]float64 {
	movieIDs := make([]int, 0, len(stats))
	for id := range stats {
		movieIDs = append(movieIDs, id)
	}

	numJobs := len(movieIDs) * (len(movieIDs) - 1) / 2
	jobs := make(chan Job, 1000)
	results := make(chan Result, 1000)
	progress := make(chan struct{}, 1000)
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			workerStats(jobs, results, progress, movieData, alpha)
		}()
	}

	go func() {
		for i := 0; i < len(movieIDs); i++ {
			for j := i + 1; j < len(movieIDs); j++ {
				jobs <- Job{
					MovieA_ID: movieIDs[i],
					MovieB_ID: movieIDs[j],
					StatsA:    stats[movieIDs[i]],
					StatsB:    stats[movieIDs[j]],
				}
			}
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
		close(progress)
	}()

	simMatrix := make(map[int]map[int]float64)

	go func() {
		counter := 0
		progressInterval := numJobs / 100
		if progressInterval == 0 {
			progressInterval = 1
		}
		for range progress {
			counter++
			if counter%progressInterval == 0 {
				fmt.Printf("Progreso: %.2f%%\r", float64(counter)*100/float64(numJobs))
			}
		}
	}()

	for result := range results {
		if _, ok := simMatrix[result.MovieA_ID]; !ok {
			simMatrix[result.MovieA_ID] = make(map[int]float64)
		}
		if _, ok := simMatrix[result.MovieB_ID]; !ok {
			simMatrix[result.MovieB_ID] = make(map[int]float64)
		}
		simMatrix[result.MovieA_ID][result.MovieB_ID] = result.Similarity
		simMatrix[result.MovieB_ID][result.MovieA_ID] = result.Similarity
	}

	fmt.Println("\nCálculo de similitudes completado.")
	return simMatrix
}

// ------------------ Recomendaciones ------------------

func generateRecommendations(userRatings map[int]float64, simMatrix map[int]map[int]float64, allMovieIDs []int, k int) []Recommendation {
	recommendations := []Recommendation{}
	for _, movieID := range allMovieIDs {
		if _, seen := userRatings[movieID]; !seen {
			predictedScore := predictScore(movieID, userRatings, simMatrix, k)
			if predictedScore > 0 {
				recommendations = append(recommendations, Recommendation{MovieID: movieID, Score: predictedScore})
			}
		}
	}
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Score > recommendations[j].Score
	})
	return recommendations
}

func predictScore(movieID int, userRatings map[int]float64, simMatrix map[int]map[int]float64, k int) float64 {
	numerator, denominator := 0.0, 0.0
	movieSimilarities, ok := simMatrix[movieID]
	if !ok {
		return 0.0
	}
	neighbors := []Recommendation{}
	for otherMovieID, similarity := range movieSimilarities {
		if _, rated := userRatings[otherMovieID]; rated {
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

func main() {
	fmt.Println("--- Recomendaciones usando matriz usuario-película + géneros ---")

	matrixPath := "user_movie_matrix.csv"
	moviePath := "movies_clean.csv"
	algorithms := []string{"cosine", "pearson", "jaccard"}
	workersList := []int{8, 12, 16}

	targetUser := 100
	k := 25
	alpha := 0.8 // peso para ratings vs géneros

	for _, algo := range algorithms {
		for _, numWorkers := range workersList {
			similarityAlgorithm = algo
			fmt.Printf("\nDataset: %s | Algoritmo: %s | Workers: %d\n", matrixPath, algo, numWorkers)

			itemRatings, err := loadItemRatingsMatrix(matrixPath)
			if err != nil {
				log.Fatal(err)
			}
			movieData, err := loadMovieData(moviePath)
			if err != nil {
				log.Fatal(err)
			}

			itemStats := precomputeItemStats(itemRatings)

			start := time.Now()
			simMatrix := calculateItemSimilaritiesStats_Parallel(itemStats, movieData, numWorkers, alpha)
			duration := time.Since(start)
			fmt.Printf("Tiempo total: %v | Películas procesadas: %d\n", duration, len(simMatrix))

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

			recommendations := generateRecommendations(userRatings, simMatrix, allMovieIDs, k)

			fmt.Println("\n--- Top 10 Películas Recomendadas ---")
			for i, rec := range recommendations {
				if i >= 10 {
					break
				}
				title := movieData[rec.MovieID].Title
				fmt.Printf("%d. %s (ID: %d) - Puntaje Previsto: %.4f\n", i+1, title, rec.MovieID, rec.Score)
			}
		}
	}
}
