// para archivos .csv
package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// estructuras base
type Movie struct {
	ID     int
	Title  string
	Genres string
	Year   string
}

type Rating struct {
	UserID  int
	MovieID int
	Rating  float64
	Time    int64
}

// lectura y limpieza

func loadMovies(path string) ([]Movie, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	reYear := regexp.MustCompile(`\s*\((\d{4})\)\s*$`)
	var movies []Movie

	for i, row := range rows {
		if i == 0 || len(row) < 3 {
			continue
		}
		id, _ := strconv.Atoi(strings.TrimSpace(row[0]))
		title := strings.TrimSpace(row[1])
		genres := strings.TrimSpace(row[2])

		year := ""
		if match := reYear.FindStringSubmatch(title); len(match) > 1 {
			year = match[1]
			title = strings.TrimSpace(reYear.ReplaceAllString(title, ""))
		}

		movies = append(movies, Movie{
			ID:     id,
			Title:  title,
			Genres: genres,
			Year:   year,
		})
	}
	return movies, nil
}

func loadRatings(path string) ([]Rating, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	var ratings []Rating
	for i, row := range rows {
		if i == 0 || len(row) < 4 {
			continue
		}
		uid, _ := strconv.Atoi(strings.TrimSpace(row[0]))
		mid, _ := strconv.Atoi(strings.TrimSpace(row[1]))
		r, _ := strconv.ParseFloat(strings.TrimSpace(row[2]), 64)
		t, _ := strconv.ParseInt(strings.TrimSpace(row[3]), 10, 64)
		ratings = append(ratings, Rating{
			UserID:  uid,
			MovieID: mid,
			Rating:  r,
			Time:    t,
		})
	}
	return ratings, nil
}

//  detección y combinación de duplicados

func buildCanonicalMap(movies []Movie) map[int]int {
	type key struct{ title, year string }
	group := make(map[key][]int)

	for _, m := range movies {
		k := key{m.Title, m.Year}
		group[k] = append(group[k], m.ID)
	}

	canonical := make(map[int]int)
	for _, ids := range group {
		sort.Ints(ids)
		mainID := ids[0]
		for _, id := range ids {
			canonical[id] = mainID
		}
	}
	return canonical
}

func mergeGenres(movies []Movie, canonical map[int]int) []Movie {
	type key struct {
		id          int
		title, year string
	}
	group := make(map[key][]string)

	for _, m := range movies {
		cid := canonical[m.ID]
		k := key{cid, m.Title, m.Year}
		group[k] = append(group[k], m.Genres)
	}

	var result []Movie
	for k, genresList := range group {
		all := strings.Join(genresList, "|")
		unique := make(map[string]bool)
		for _, g := range strings.Split(all, "|") {
			g = strings.TrimSpace(g)
			if g != "" && g != "(no genres listed)" {
				unique[g] = true
			}
		}
		var merged []string
		for g := range unique {
			merged = append(merged, g)
		}
		sort.Strings(merged)
		result = append(result, Movie{
			ID:     k.id,
			Title:  k.title,
			Year:   k.year,
			Genres: strings.Join(merged, "|"),
		})
	}
	return result
}

// Normaliza ratings a [0,1] usando min-max
func normalizeRatings(ratings []Rating) []Rating {
	if len(ratings) == 0 {
		return ratings
	}
	minR, maxR := ratings[0].Rating, ratings[0].Rating
	for _, r := range ratings {
		if r.Rating < minR {
			minR = r.Rating
		}
		if r.Rating > maxR {
			maxR = r.Rating
		}
	}
	scale := maxR - minR
	for i := range ratings {
		if scale > 0 {
			ratings[i].Rating = (ratings[i].Rating - minR) / scale
		} else {
			ratings[i].Rating = 0
		}
	}
	return ratings
}

// Genera matriz usuario-película completa en CSV
func generateUserMovieMatrix(path string, ratings []Rating) error {
	matrix := make(map[int]map[int]float64)
	users, movies := make(map[int]bool), make(map[int]bool)

	for _, r := range ratings {
		if matrix[r.UserID] == nil {
			matrix[r.UserID] = make(map[int]float64)
		}
		matrix[r.UserID][r.MovieID] = r.Rating
		users[r.UserID] = true
		movies[r.MovieID] = true
	}

	userIDs := sortedKeys(users)
	movieIDs := sortedKeys(movies)

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	w := csv.NewWriter(file)
	defer w.Flush()

	header := []string{"user_id"}
	for _, mid := range movieIDs {
		header = append(header, strconv.Itoa(mid))
	}
	w.Write(header)

	for _, uid := range userIDs {
		row := []string{strconv.Itoa(uid)}
		for _, mid := range movieIDs {
			if val, ok := matrix[uid][mid]; ok {
				row = append(row, fmt.Sprintf("%.3f", val))
			} else {
				row = append(row, "")
			}
		}
		w.Write(row)
	}
	return nil
}

func sortedKeys(m map[int]bool) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// guardado de datos limpios

func saveMovies(path string, movies []Movie) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{"movie_id", "title", "genres", "year"})
	for _, m := range movies {
		title := strings.ReplaceAll(m.Title, ",", "")
		title = strings.ReplaceAll(title, "\"", "")
		w.Write([]string{
			strconv.Itoa(m.ID),
			title,
			m.Genres,
			m.Year,
		})
	}
	return nil
}

func saveRatings(path string, ratings []Rating) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{"user_id", "movie_id", "rating", "timestamp"})
	for _, r := range ratings {
		w.Write([]string{
			strconv.Itoa(r.UserID),
			strconv.Itoa(r.MovieID),
			fmt.Sprintf("%.3f", r.Rating),
			strconv.FormatInt(r.Time, 10),
		})
	}
	return nil
}

// main

func main() {
	// limpia data
	movies, _ := loadMovies("./ml-20m/movies.csv")
	fmt.Printf("Películas cargadas: %d\n", len(movies))

	ratings, _ := loadRatings("./ml-20m/ratings.csv")
	fmt.Printf("Ratings cargados: %d\n", len(ratings))

	// combina duplicados
	canonical := buildCanonicalMap(movies)
	for i := range ratings {
		if cid, ok := canonical[ratings[i].MovieID]; ok {
			ratings[i].MovieID = cid
		}
	}
	mvFinal := mergeGenres(movies, canonical)

	// normaliza
	ratings = normalizeRatings(ratings)
	fmt.Println("Ratings normalizados en rango [0,1]")

	// guardamos CSVs limpios
	saveMovies("movies_clean_20.csv", mvFinal)
	saveRatings("ratings_clean_20.csv", ratings)
	fmt.Println("Archivos limpios guardados ✅")

	// se genera matriz usuario-película completa
	err := generateUserMovieMatrix("user_movie_matrix_20.csv", ratings)
	if err != nil {
		panic(err)
	}
	fmt.Println("Matriz usuario-película generada ✅")

	fmt.Printf("Usuarios únicos: %d | Películas únicas: %d\n",
		countUniqueUsers(ratings), countUniqueMovies(ratings))
}

// Funciones de conteo
func countUniqueUsers(r []Rating) int {
	m := make(map[int]bool)
	for _, x := range r {
		m[x.UserID] = true
	}
	return len(m)
}

func countUniqueMovies(r []Rating) int {
	m := make(map[int]bool)
	for _, x := range r {
		m[x.MovieID] = true
	}
	return len(m)
}
