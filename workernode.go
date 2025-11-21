package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
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

	msg, _ := bufio.NewReader(conn).ReadString('\n')
	msg = strings.TrimSpace(msg)

	if msg == "PING" {
		conn.Write([]byte("PONG\n"))
		return
	}

	parts := strings.Split(msg, " ")
	if len(parts) != 3 || parts[0] != "SIMILARITY" {
		conn.Write([]byte("ERROR\n"))
		return
	}

	a, _ := strconv.Atoi(parts[1])
	b, _ := strconv.Atoi(parts[2])

	sim := computeSimilarity(a, b)
	conn.Write([]byte(fmt.Sprintf("RESULT %.6f\n", sim)))
}
