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
	_ "github.com/lib/pq"
)

// ═══════════════════════════════════════════════════════════
//  DATA TYPES
// ═══════════════════════════════════════════════════════════

type Movie struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Genre string `json:"genre"`
	Year  int    `json:"year"`
	Stock int    `json:"stock"`
}

// Every event published to Kafka has this shape
type KafkaEvent struct {
	Event string `json:"event"` // e.g. "movie.created"
	Movie Movie  `json:"movie"`
}

// ═══════════════════════════════════════════════════════════
//  GLOBAL CONNECTIONS
// ═══════════════════════════════════════════════════════════

var db       *sql.DB
var producer sarama.SyncProducer

// ═══════════════════════════════════════════════════════════
//  MAIN
// ═══════════════════════════════════════════════════════════

func main() {
	log.Println("🎬 Inventory Service starting...")
	initDB()
	initKafka()
	startServer()
}

// ═══════════════════════════════════════════════════════════
//  DATABASE SETUP
// ═══════════════════════════════════════════════════════════

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
		log.Fatal("❌ DB connection failed:", err)
	}

	// Ping to verify connection is alive
	if err = db.Ping(); err != nil {
		log.Fatal("❌ DB ping failed:", err)
	}

	// Create table if it doesn't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS movies (
			id    SERIAL PRIMARY KEY,
			title TEXT NOT NULL,
			genre TEXT,
			year  INT,
			stock INT DEFAULT 0
		)
	`)
	if err != nil {
		log.Fatal("❌ Failed to create movies table:", err)
	}

	log.Println("✅ PostgreSQL connected and table ready")
}

// ═══════════════════════════════════════════════════════════
//  KAFKA SETUP
//  Inventory is a PRODUCER — it sends events to Kafka
// ═══════════════════════════════════════════════════════════

func initKafka() {
	config := sarama.NewConfig()

	// Wait for all brokers to confirm message received
	config.Producer.RequiredAcks = sarama.WaitForAll

	// Retry up to 5 times if publish fails
	config.Producer.Retry.Max = 5

	// Must be true to use SyncProducer
	config.Producer.Return.Successes = true

	brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")

	var err error
	producer, err = sarama.NewSyncProducer(brokers, config)
	if err != nil {
		log.Fatal("❌ Kafka producer failed:", err)
	}

	log.Println("✅ Kafka producer connected →", brokers)
}

// ═══════════════════════════════════════════════════════════
//  HTTP SERVER
// ═══════════════════════════════════════════════════════════

func startServer() {
	mux := http.NewServeMux()

	mux.HandleFunc("/health",  handleHealth)   // K8s liveness/readiness probe
	mux.HandleFunc("/movies",  handleMovies)   // GET all, POST create
	mux.HandleFunc("/movies/", handleMovie)    // GET one, PUT update stock, DELETE

	log.Println("🚀 Inventory service listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

// ═══════════════════════════════════════════════════════════
//  HANDLERS
// ═══════════════════════════════════════════════════════════

// GET /health
func handleHealth(w http.ResponseWriter, r *http.Request) {
	// Also check DB is still alive
	if err := db.Ping(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "unhealthy",
			"reason": "db unreachable",
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GET /movies        → list all movies
// POST /movies       → create a movie  → publishes "movie.created" to Kafka
func handleMovies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {

	case http.MethodGet:
		rows, err := db.Query(
			"SELECT id, title, genre, year, stock FROM movies ORDER BY id",
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var movies []Movie
		for rows.Next() {
			var m Movie
			rows.Scan(&m.ID, &m.Title, &m.Genre, &m.Year, &m.Stock)
			movies = append(movies, m)
		}

		// Return empty array instead of null
		if movies == nil {
			movies = []Movie{}
		}
		json.NewEncoder(w).Encode(movies)

	case http.MethodPost:
		var m Movie
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		// Save to PostgreSQL
		err := db.QueryRow(
			`INSERT INTO movies(title, genre, year, stock)
			 VALUES($1, $2, $3, $4) RETURNING id`,
			m.Title, m.Genre, m.Year, m.Stock,
		).Scan(&m.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// ── Publish to Kafka ──────────────────────────────────────────
		// This tells Order Service (and any other listener) that a new
		// movie exists and is available for ordering
		publishEvent("movie.created", m)

		log.Printf("🎬 Created movie: [%d] %s (stock: %d)", m.ID, m.Title, m.Stock)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(m)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET    /movies/:id  → get one movie
// PUT    /movies/:id  → update stock  → publishes "movie.stock_updated" to Kafka
// DELETE /movies/:id  → delete movie  → publishes "movie.deleted" to Kafka
func handleMovie(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/movies/"))
	if err != nil {
		http.Error(w, "invalid movie id", http.StatusBadRequest)
		return
	}

	switch r.Method {

	case http.MethodGet:
		var m Movie
		err := db.QueryRow(
			"SELECT id, title, genre, year, stock FROM movies WHERE id=$1", id,
		).Scan(&m.ID, &m.Title, &m.Genre, &m.Year, &m.Stock)
		if err == sql.ErrNoRows {
			http.Error(w, "movie not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(m)

	case http.MethodPut:
		// Update stock only
		// Body: {"stock": 25}
		var body struct {
			Stock int `json:"stock"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		var m Movie
		err := db.QueryRow(
			`UPDATE movies SET stock=$1 WHERE id=$2
			 RETURNING id, title, genre, year, stock`,
			body.Stock, id,
		).Scan(&m.ID, &m.Title, &m.Genre, &m.Year, &m.Stock)
		if err == sql.ErrNoRows {
			http.Error(w, "movie not found", http.StatusNotFound)
			return
		}

		// ── Publish stock update to Kafka ─────────────────────────────
		// Order Service listens for this to keep its cache in sync
		publishEvent("movie.stock_updated", m)

		log.Printf("📦 Stock updated: [%d] %s → stock: %d", m.ID, m.Title, m.Stock)
		json.NewEncoder(w).Encode(m)

	case http.MethodDelete:
		var m Movie
		err := db.QueryRow(
			"DELETE FROM movies WHERE id=$1 RETURNING id, title, genre, year, stock", id,
		).Scan(&m.ID, &m.Title, &m.Genre, &m.Year, &m.Stock)
		if err == sql.ErrNoRows {
			http.Error(w, "movie not found", http.StatusNotFound)
			return
		}

		// ── Publish deletion to Kafka ─────────────────────────────────
		// Order Service must remove this from its cache
		publishEvent("movie.deleted", m)

		log.Printf("🗑️  Deleted movie: [%d] %s", m.ID, m.Title)
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ═══════════════════════════════════════════════════════════
//  KAFKA PUBLISHER
//  All events go to the same topic: "movie.events"
//  The event field tells consumers WHAT happened
// ═══════════════════════════════════════════════════════════

func publishEvent(eventType string, movie Movie) {
	event := KafkaEvent{
		Event: eventType,
		Movie: movie,
	}

	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("❌ Failed to marshal Kafka event: %v", err)
		return
	}

	msg := &sarama.ProducerMessage{
		Topic: "movie.events",
		// Key = movie ID (ensures same movie's events go to same partition)
		Key:   sarama.StringEncoder(fmt.Sprintf("%d", movie.ID)),
		Value: sarama.StringEncoder(payload),
	}

	partition, offset, err := producer.SendMessage(msg)
	if err != nil {
		log.Printf("❌ Kafka publish failed: %v", err)
		return
	}

	log.Printf("📤 Kafka event published: event=%s movie=%s partition=%d offset=%d",
		eventType, movie.Title, partition, offset)
}