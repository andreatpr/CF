package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"
)

var itemStats map[int]ItemStats
var movieData map[int]MovieData
var itemRatings ItemRatings

func computeSimilarity(aID, bID int) float64 {
	a := itemStats[aID]
	b := itemStats[bID]

	simR := CosineSimilarityStats(a, b)
	simG := GenreSimilarity(movieData[aID].Genres, movieData[bID].Genres)

	return 0.8*simR + 0.2*simG
}

func main() {
	port := flag.String("port", "9000", "port")
	flag.Parse()

	itemRatings, _ = LoadItemRatingsMatrix("./analisisdata/resultados/20M/user_movie_matrix_20.csv")
	movieData, _ = LoadMovieData("./analisisdata/resultados/20M/movies_clean_20.csv")

	itemStats = PrecomputeItemStats(itemRatings)

	ln, _ := net.Listen("tcp", ":"+*port)
	fmt.Println("Worker escuchando en puerto", *port)

	for {
		conn, _ := ln.Accept()
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
