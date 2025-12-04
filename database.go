package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// --- Inicialización ---

func initDB() {
	// 1. Conectar a Redis
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	rdb = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	ctxT, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if _, err := rdb.Ping(ctxT).Result(); err != nil {
		log.Printf("⚠️ No se pudo conectar a Redis: %v", err)
	} else {
		fmt.Println("✔ Conectado a Redis")
	}

	// 2. Conectar a MongoDB
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	clientOptions := options.Client().ApplyURI(mongoURI)
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Fatal("Error creando cliente Mongo:", err)
	}

	// Verificar conexión
	err = client.Ping(ctxT, nil)
	if err != nil {
		log.Printf("⚠️ No se pudo conectar a MongoDB: %v", err)
	} else {
		fmt.Println("✔ Conectado a MongoDB")
	}

	mongoClient = client
}

// --- Migración de Datos (CSV -> Mongo) ---

func MigrateMoviesToMongo(path string) {
	// Verificar conexión antes de intentar nada
	if mongoClient == nil {
		log.Println("❌ MongoDB no inicializado, saltando migración.")
		return
	}

	coll := mongoClient.Database("recsys").Collection("movies")

	// Verificar si ya existen datos
	count, _ := coll.CountDocuments(ctx, bson.M{})
	if count > 0 {
		fmt.Println("ℹ️ Las películas ya están en MongoDB.")
		return
	}

	fmt.Println("🚀 Iniciando migración de películas a MongoDB...")

	// Usamos la función LoadMovieData que ya tienes en rec.go
	movies, err := LoadMovieData(path)
	if err != nil {
		log.Printf("Error leyendo CSV para migración: %v", err)
		return
	}

	var docs []interface{}
	for id, data := range movies {
		docs = append(docs, bson.M{
			"_id":    id,
			"title":  data.Title,
			"genres": data.Genres,
		})

		if len(docs) >= 1000 {
			_, err := coll.InsertMany(ctx, docs)
			if err != nil {
				log.Printf("Error insertando lote: %v", err)
			}
			docs = nil
		}
	}
	if len(docs) > 0 {
		coll.InsertMany(ctx, docs)
	}
	fmt.Println("✔ Migración completada.")
}
