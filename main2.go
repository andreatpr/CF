package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

// ------------------ Estructuras ------------------

type ItemRatings map[int]map[int]float64
type MovieTitles map[int]string

type Recommendation struct {
	MovieID int
	Score   float64
}

type Job struct {
	MovieA_ID int
	MovieB_ID int
	VecA      map[int]float64
	VecB      map[int]float64
}
type Result struct {
	MovieA_ID  int
	MovieB_ID  int
	Similarity float64
}

// ------------------ Variables globales ------------------

var similarityAlgorithm = "cosine" // "cosine", "pearson", "jaccard"

// ------------------ Funciones de similitud ------------------

func cosineSimilarity(vecA, vecB map[int]float64) float64 {
	dotProduct, normA, normB := 0.0, 0.0, 0.0
	for key, valA := range vecA {
		normA += valA * valA
		if valB, ok := vecB[key]; ok {
			dotProduct += valA * valB
		}
	}
	for _, valB := range vecB {
		normB += valB * valB
	}
	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return dotProduct / (normA * normB)
}

func pearsonCorrelation(vecA, vecB map[int]float64) float64 {
	commonKeys := []int{}
	sumA, sumB := 0.0, 0.0
	for key, valA := range vecA {
		if valB, ok := vecB[key]; ok {
			commonKeys = append(commonKeys, key)
			sumA += valA
			sumB += valB
		}
	}
	n := float64(len(commonKeys))
	if n == 0 {
		return 0.0
	}
	meanA := sumA / n
	meanB := sumB / n
	num, sumSqA, sumSqB := 0.0, 0.0, 0.0
	for _, key := range commonKeys {
		cA := vecA[key] - meanA
		cB := vecB[key] - meanB
		num += cA * cB
		sumSqA += cA * cA
		sumSqB += cB * cB
	}
	den := math.Sqrt(sumSqA) * math.Sqrt(sumSqB)
	if den == 0 {
		return 0.0
	}
	return num / den
}

func jaccardIndex(vecA, vecB map[int]float64) float64 {
	intersection := 0.0
	unionSet := make(map[int]bool)
	for key := range vecA {
		unionSet[key] = true
		if _, ok := vecB[key]; ok {
			intersection++
		}
	}
	for key := range vecB {
		unionSet[key] = true
	}
	union := float64(len(unionSet))
	if union == 0 {
		return 0.0
	}
	return intersection / union
}

// ------------------ Carga de datos ------------------

func loadItemRatings(path string) (ItemRatings, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error al abrir el archivo de ratings: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Read()

	itemRatings := make(ItemRatings)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		userID, _ := strconv.Atoi(record[0])
		movieID, _ := strconv.Atoi(record[1])
		rating, _ := strconv.ParseFloat(record[2], 64)

		if _, ok := itemRatings[movieID]; !ok {
			itemRatings[movieID] = make(map[int]float64)
		}
		itemRatings[movieID][userID] = rating
	}
	fmt.Printf("Cargados ratings para %d películas.\n", len(itemRatings))
	return itemRatings, nil
}

func loadMovieTitles(path string) (MovieTitles, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error al abrir el archivo de películas: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Read()

	titles := make(MovieTitles)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		movieID, _ := strconv.Atoi(record[0])
		titles[movieID] = record[1]
	}
	fmt.Printf("Cargados %d títulos de películas.\n", len(titles))
	return titles, nil
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

// ------------------ Cálculo paralelo ------------------

func worker(jobs <-chan Job, results chan<- Result, progress chan<- struct{}) {
	for job := range jobs {
		var similarity float64
		switch similarityAlgorithm {
		case "pearson":
			similarity = pearsonCorrelation(job.VecA, job.VecB)
		case "jaccard":
			similarity = jaccardIndex(job.VecA, job.VecB)
		default:
			similarity = cosineSimilarity(job.VecA, job.VecB)
		}

		// 🔹 Notifica que se terminó un trabajo (aunque no haya resultado)
		progress <- struct{}{}

		if similarity > 0.1 {
			results <- Result{
				MovieA_ID:  job.MovieA_ID,
				MovieB_ID:  job.MovieB_ID,
				Similarity: similarity,
			}
		}
	}
}

func calculateItemSimilarities_Parallel(ratings ItemRatings, numWorkers int) map[int]map[int]float64 {
	movieIDs := make([]int, 0, len(ratings))
	for id := range ratings {
		movieIDs = append(movieIDs, id)
	}

	numJobs := len(movieIDs) * (len(movieIDs) - 1) / 2
	jobs := make(chan Job, 1000)
	results := make(chan Result, 1000)
	progress := make(chan struct{}, 1000)
	var wg sync.WaitGroup

	// --- Lanzar los workers ---
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker(jobs, results, progress)
		}()
	}

	// --- Enviar trabajos ---
	go func() {
		for i := 0; i < len(movieIDs); i++ {
			for j := i + 1; j < len(movieIDs); j++ {
				jobs <- Job{
					MovieA_ID: movieIDs[i],
					MovieB_ID: movieIDs[j],
					VecA:      ratings[movieIDs[i]],
					VecB:      ratings[movieIDs[j]],
				}
			}
		}
		close(jobs)
	}()

	// --- Cerrar canales al terminar ---
	go func() {
		wg.Wait()
		close(results)
		close(progress)
	}()

	simMatrix := make(map[int]map[int]float64)

	// --- Monitorear progreso en paralelo ---
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

	// --- Guardar resultados de similitud ---
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

	fmt.Println("\n✅ Cálculo de similitudes completado.")
	return simMatrix
}

// ------------------ MAIN ------------------

func main() {
	fmt.Println("--- Pruebas de Algoritmos y Concurrencia ---")

	datasets := []string{"10M", "20M", "25M"}
	algorithms := []string{"cosine", "pearson", "jaccard"}
	workersList := []int{2, 4, 8}

	targetUserID := 100
	k := 25

	for _, dataset := range datasets {
		for _, algo := range algorithms {
			for _, numWorkers := range workersList {

				similarityAlgorithm = algo
				fmt.Printf("\n Dataset: %s |  Algoritmo: %s |  Workers: %d\n", dataset, algo, numWorkers)

				itemRatings, err := loadItemRatings(fmt.Sprintf("%s/%s/ratings_clean.csv", dataset, dataset))
				if err != nil {
					fmt.Println("Error al cargar ratings:", err)
					continue
				}
				movieTitles, _ := loadMovieTitles(fmt.Sprintf("%s/%s/movies_clean.csv", dataset, dataset))

				start := time.Now()
				simMatrix := calculateItemSimilarities_Parallel(itemRatings, numWorkers)
				duration := time.Since(start)
				fmt.Printf(" Tiempo total: %v |  Películas procesadas: %d\n", duration, len(simMatrix))

				// 🔹 Recomendaciones reales
				userRatings := make(map[int]float64)
				for movieID, ratings := range itemRatings {
					if rating, ok := ratings[targetUserID]; ok {
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
					title := movieTitles[rec.MovieID]
					fmt.Printf("%d. %s (ID: %d) - Puntaje Previsto: %.4f\n", i+1, title, rec.MovieID, rec.Score)
				}
			}
		}
	}
}
