package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/IBM/sarama"

	// Kafka client
	_ "github.com/lib/pq" // PostgreSQL driver
)

// ─── Data Types ───────────────────────────────────────
type Movie struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Genre string `json:"genre"`
	Year  int    `json:"year"`
	Stock int    `json:"stock"`
}

// ─── Global Connections ───────────────────────────────
var db *sql.DB
var producer sarama.SyncProducer

// ─── Main ─────────────────────────────────────────────
func main() {
	initDB()      // Connect to PostgreSQL
	initKafka()   // Connect to Kafka
	startServer() // Start HTTP server
}

func initDB() {
	dsn := fmt.Sprintf(
		"host=%s port=5432 user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
	)
	var err error
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS movies (
        id SERIAL PRIMARY KEY,
        title TEXT NOT NULL,
        genre TEXT,
        year INT,
        stock INT DEFAULT 0
    )`)
	log.Println("DB connected")
}

func initKafka() {
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	var err error
	producer, err = sarama.NewSyncProducer(brokers, config)
	if err != nil {
		log.Fatal("Kafka producer error:", err)
	}
	log.Println("Kafka connected")
}

func startServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/movies", handleMovies) // GET all, POST create
	mux.HandleFunc("/movies/", handleMovie) // GET one, PUT update, DELETE
	mux.HandleFunc("/health", handleHealth) // Health check for K8s
	log.Println("Inventory service running on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// ─── HTTP Handlers ────────────────────────────────────

func handleMovies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		rows, _ := db.Query("SELECT id, title, genre, year, stock FROM movies")
		defer rows.Close()
		var movies []Movie
		for rows.Next() {
			var m Movie
			rows.Scan(&m.ID, &m.Title, &m.Genre, &m.Year, &m.Stock)
			movies = append(movies, m)
		}
		json.NewEncoder(w).Encode(movies)
	case "POST":
		var m Movie
		json.NewDecoder(r.Body).Decode(&m)
		db.QueryRow("INSERT INTO movies(title,genre,year,stock) VALUES($1,$2,$3,$4) RETURNING id",
			m.Title, m.Genre, m.Year, m.Stock).Scan(&m.ID)
		publishEvent("movie.created", m)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(m)
	}
}

func handleMovie(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/movies/"))
	switch r.Method {
	case "GET":
		var m Movie
		db.QueryRow("SELECT id,title,genre,year,stock FROM movies WHERE id=$1", id).
			Scan(&m.ID, &m.Title, &m.Genre, &m.Year, &m.Stock)
		json.NewEncoder(w).Encode(m)
	case "DELETE":
		db.Exec("DELETE FROM movies WHERE id=$1", id)
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ─── Kafka Publisher ──────────────────────────────────
func publishEvent(eventType string, movie Movie) {
	payload, _ := json.Marshal(map[string]interface{}{
		"event": eventType,
		"movie": movie,
	})
	msg := &sarama.ProducerMessage{
		Topic: "movie.events",
		Value: sarama.StringEncoder(payload),
	}
	producer.SendMessage(msg)
	log.Printf("Published event: %s for movie: %s", eventType, movie.Title)
}
