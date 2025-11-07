package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"time"
)

// Mapa para cargar los ratings de forma optimizada para Item-based CF
type ItemRatings map[int]map[int]float64

// Mapa para guardar los títulos de las peliculas
type MovieTitles map[int]string

// Estructura para una recomendación
type Recommendation struct {
	MovieID int
	Score   float64
}

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
	fmt.Printf("Cargados ratings para %d peliculas.\n", len(itemRatings))
	return itemRatings, nil
}

// loadMovieTitles carga los títulos desde el CSV limpio
func loadMovieTitles(path string) (MovieTitles, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error al abrir el archivo de peliculas: %w", err)
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
	fmt.Printf("Cargados %d títulos de peliculas.\n", len(titles))
	return titles, nil
}

// generateRecommendations genera una lista de peluculas recomendadas para un usuario
// Utiliza k-NN
func generateRecommendations(userRatings map[int]float64, simMatrix map[int]map[int]float64, allMovieIDs []int, k int) []Recommendation {

	recommendations := []Recommendation{}

	// Para cada pelicula que el usuario no ha visto
	for _, movieID := range allMovieIDs {
		if _, seen := userRatings[movieID]; !seen {

			// predece el rating que le dará
			predictedScore := predictScore(movieID, userRatings, simMatrix, k)

			if predictedScore > 0 {
				recommendations = append(recommendations, Recommendation{MovieID: movieID, Score: predictedScore})
			}
		}
	}

	// Ordenar las recomendaciones de mayor a menor
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Score > recommendations[j].Score
	})

	return recommendations
}

// predictScore calcula el rating ponderado para una película basado en las peliculas que el usuario ya ha calificado
func predictScore(movieID int, userRatings map[int]float64, simMatrix map[int]map[int]float64, k int) float64 {
	numerator := 0.0
	denominator := 0.0

	// Obtener todas las peliculas similares a 'movieID'
	movieSimilarities, ok := simMatrix[movieID]
	if !ok {
		return 0.0
	}

	neighbors := []Recommendation{}
	for otherMovieID, similarity := range movieSimilarities {
		// Nos interesan solo las peliculas similares que el usuario ha calificado
		if _, rated := userRatings[otherMovieID]; rated {
			neighbors = append(neighbors, Recommendation{MovieID: otherMovieID, Score: similarity})
		}
	}

	// Ordenar los vecinos por similitud para encontrar los k más cercanos
	sort.Slice(neighbors, func(i, j int) bool {
		return neighbors[i].Score > neighbors[j].Score
	})

	// Limitar al top k vecinos
	if len(neighbors) > k {
		neighbors = neighbors[:k]
	}

	// Calcular el puntaje ponderado
	for _, neighbor := range neighbors {
		similarity := neighbor.Score
		neighborRating := userRatings[neighbor.MovieID]

		numerator += similarity * neighborRating
		denominator += similarity
	}

	if denominator == 0 {
		return 0.0
	}

	return numerator / denominator
}

// Estructuras para el Worker Pool
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

// worker es un goroutine que procesa trabajos del canal 'jobs' y envía resultados a 'results'
func worker(jobs <-chan Job, results chan<- Result) {
	for job := range jobs {
		// se usan las funciones de similitud
		similarity := cosineSimilarity(job.VecA, job.VecB)
		if similarity > 0.1 { // Un umbral para no guardar similitudes muy bajas
			results <- Result{
				MovieA_ID:  job.MovieA_ID,
				MovieB_ID:  job.MovieB_ID,
				Similarity: similarity,
			}
		}
	}
}

// calculateItemSimilarities_Parallel calcula la matriz de similitud usando goroutines
func calculateItemSimilarities_Parallel(ratings ItemRatings, numWorkers int) map[int]map[int]float64 {

	movieIDs := make([]int, 0, len(ratings))
	for id := range ratings {
		movieIDs = append(movieIDs, id)
	}

	numJobs := len(movieIDs) * (len(movieIDs) - 1) / 2
	jobs := make(chan Job, numJobs)
	results := make(chan Result, numJobs)

	// Iniciar los workers
	for w := 0; w < numWorkers; w++ {
		go worker(jobs, results)
	}

	// Enviar todos los trabajos al jobs
	for i := 0; i < len(movieIDs); i++ {
		for j := i + 1; j < len(movieIDs); j++ {
			movieA_ID := movieIDs[i]
			movieB_ID := movieIDs[j]
			jobs <- Job{
				MovieA_ID: movieA_ID,
				MovieB_ID: movieB_ID,
				VecA:      ratings[movieA_ID],
				VecB:      ratings[movieB_ID],
			}
		}
	}
	close(jobs)

	// Recolectar los resultados
	simMatrix := make(map[int]map[int]float64)
	for i := 0; i < numJobs; i++ {
		select {
		case result := <-results:
			if simMatrix[result.MovieA_ID] == nil {
				simMatrix[result.MovieA_ID] = make(map[int]float64)
			}
			if simMatrix[result.MovieB_ID] == nil {
				simMatrix[result.MovieB_ID] = make(map[int]float64)
			}
			simMatrix[result.MovieA_ID][result.MovieB_ID] = result.Similarity
			simMatrix[result.MovieB_ID][result.MovieA_ID] = result.Similarity
		default:
			// Si no hay más resultados, salimos del bucle.
			// Esto evita que el programa se quede colgado si pocos pares tienen similitud.
			goto endLoop
		}
	}
endLoop:

	return simMatrix
}

func main() {
	fmt.Println("--- Inicio del Proceso de Recomendacion ---")

	// --- CARGA DE DATOS ---
	itemRatings, err := loadItemRatings("ratings_clean.csv")
	if err != nil {
		panic(err)
	}
	movieTitles, err := loadMovieTitles("movies_clean.csv")
	if err != nil {
		panic(err)
	}

	numWorkers := 8 // 8 workers
	fmt.Printf("Iniciando cálculo de similitud con %d workers...\n", numWorkers)

	start := time.Now()
	simMatrix := calculateItemSimilarities_Parallel(itemRatings, numWorkers)
	duration := time.Since(start)

	fmt.Printf("Matriz de similitud calculada en: %s\n", duration)
	fmt.Printf("Se encontraron similitudes para %d películas.\n\n", len(simMatrix))

	targetUserID := 100
	k := 25 // Número de vecinos

	// Obtenemos los ratings del usuario
	userRatings := make(map[int]float64)
	for movieID, ratings := range itemRatings {
		if rating, ok := ratings[targetUserID]; ok {
			userRatings[movieID] = rating
		}
	}

	// Necesitamos la lista completa de películas para saber cuáles no ha visto el usuario
	allMovieIDs := make([]int, 0, len(itemRatings))
	for id := range itemRatings {
		allMovieIDs = append(allMovieIDs, id)
	}

	fmt.Printf("Generando recomendaciones para el Usuario ID: %d (k=%d)\n", targetUserID, k)
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
