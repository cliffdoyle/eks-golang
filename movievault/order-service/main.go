package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/IBM/sarama"
	_ "github.com/lib/pq"
)

// ═══════════════════════════════════════════════════════════
//  DATA TYPES
// ═══════════════════════════════════════════════════════════

type Order struct {
	ID       int    `json:"id"`
	MovieID  int    `json:"movie_id"`
	Customer string `json:"customer"`
	Quantity int    `json:"quantity"`
	Status   string `json:"status"`
}

// Local cache of movies received FROM Kafka
// Order service never talks to Inventory DB directly
type CachedMovie struct {
	ID    int
	Title string
	Stock int
}

// Shape of events coming FROM Kafka
type KafkaEvent struct {
	Event string `json:"event"`
	Movie struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
		Genre string `json:"genre"`
		Year  int    `json:"year"`
		Stock int    `json:"stock"`
	} `json:"movie"`
}

// ═══════════════════════════════════════════════════════════
//  GLOBAL CONNECTIONS
// ═══════════════════════════════════════════════════════════

var db *sql.DB

// ═══════════════════════════════════════════════════════════
//  MAIN
// ═══════════════════════════════════════════════════════════

func main() {
	log.Println("🛒 Order Service starting...")
	initDB()

	// Run Kafka consumer in background goroutine
	// It runs forever alongside the HTTP server
	go consumeKafkaEvents()

	startServer()
}

// ═══════════════════════════════════════════════════════════
//  DATABASE SETUP
//  Order service has TWO tables:
//  1. orders           — the actual orders placed by customers
//  2. available_movies — a LOCAL CACHE built from Kafka events
//                        (never queries Inventory DB directly!)
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
	if err = db.Ping(); err != nil {
		log.Fatal("❌ DB ping failed:", err)
	}

	// Orders table — what customers have ordered
	db.Exec(`
		CREATE TABLE IF NOT EXISTS orders (
			id       SERIAL PRIMARY KEY,
			movie_id INT NOT NULL,
			customer TEXT NOT NULL,
			quantity INT DEFAULT 1,
			status   TEXT DEFAULT 'pending'
		)
	`)

	// ── The Kafka cache table ─────────────────────────────────────────
	// This is populated ONLY by Kafka events from Inventory Service
	// Order service uses this to:
	//   1. Check if a movie exists before accepting an order
	//   2. Check if stock is available
	//   3. Reject orders for deleted movies
	db.Exec(`
		CREATE TABLE IF NOT EXISTS available_movies (
			id    INT PRIMARY KEY,
			title TEXT,
			stock INT DEFAULT 0
		)
	`)

	log.Println("✅ PostgreSQL connected — orders + available_movies tables ready")
}

// ═══════════════════════════════════════════════════════════
//  KAFKA CONSUMER
//  Order Service is a CONSUMER — it listens to Kafka
//  and reacts to events published by Inventory Service
//
//  This runs as a goroutine (background thread)
//  It never stops — it loops forever waiting for messages
// ═══════════════════════════════════════════════════════════

func consumeKafkaEvents() {
	config := sarama.NewConfig()

	// Start reading from the oldest message (catch up on missed events)
	config.Consumer.Offsets.Initial = sarama.OffsetOldest

	brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")

	consumer, err := sarama.NewConsumer(brokers, config)
	if err != nil {
		log.Fatal("❌ Kafka consumer failed to start:", err)
	}
	defer consumer.Close()

	// Subscribe to the "movie.events" topic
	// Partition 0 — fine for our single-broker setup
	partConsumer, err := consumer.ConsumePartition(
		"movie.events", 0, sarama.OffsetOldest,
	)
	if err != nil {
		log.Fatal("❌ Failed to consume topic movie.events:", err)
	}
	defer partConsumer.Close()

	log.Println("👂 Kafka consumer listening on topic: movie.events")

	// ── Event loop — runs forever ─────────────────────────────────────
	for {
		select {

		// A new message arrived from Kafka
		case msg := <-partConsumer.Messages():
			log.Printf("📨 Kafka message received — offset: %d", msg.Offset)
			handleKafkaEvent(msg.Value)

		// An error from the Kafka broker
		case err := <-partConsumer.Errors():
			log.Printf("❌ Kafka error: %v", err)
		}
	}
}

// ── Process each Kafka event ──────────────────────────────────────────────────
//
//	Inventory publishes 3 event types — we handle all 3:
//	  "movie.created"       → add to our local cache
//	  "movie.stock_updated" → update stock in our cache
//	  "movie.deleted"       → remove from our cache
func handleKafkaEvent(data []byte) {
	var event KafkaEvent
	if err := json.Unmarshal(data, &event); err != nil {
		log.Printf("❌ Failed to parse Kafka event: %v", err)
		return
	}

	movie := event.Movie
	log.Printf("📋 Event type: %s | Movie: [%d] %s | Stock: %d",
		event.Event, movie.ID, movie.Title, movie.Stock)

	switch event.Event {

	case "movie.created":
		// ── A new movie was added to inventory ───────────────────────
		// Insert into our local cache so we can accept orders for it
		_, err := db.Exec(`
			INSERT INTO available_movies (id, title, stock)
			VALUES ($1, $2, $3)
			ON CONFLICT (id) DO UPDATE
			SET title = $2, stock = $3
		`, movie.ID, movie.Title, movie.Stock)

		if err != nil {
			log.Printf("❌ Failed to cache movie: %v", err)
		} else {
			log.Printf("✅ Movie cached: [%d] %s (stock: %d) — orders now accepted",
				movie.ID, movie.Title, movie.Stock)
		}

	case "movie.stock_updated":
		// ── Stock level changed in inventory ─────────────────────────
		// Update our cache so order validation uses correct stock level
		result, err := db.Exec(
			"UPDATE available_movies SET stock=$1 WHERE id=$2",
			movie.Stock, movie.ID,
		)
		if err != nil {
			log.Printf("❌ Failed to update stock cache: %v", err)
		} else {
			rows, _ := result.RowsAffected()
			if rows > 0 {
				log.Printf("📦 Stock updated in cache: movie [%d] → %d units",
					movie.ID, movie.Stock)
				// If stock dropped to 0, cancel all pending orders for this movie
				if movie.Stock == 0 {
					cancelPendingOrders(movie.ID, movie.Title)
				}
			}
		}

	case "movie.deleted":
		// ── Movie was removed from inventory ─────────────────────────
		// Remove from cache — no more orders can be placed
		db.Exec("DELETE FROM available_movies WHERE id=$1", movie.ID)
		log.Printf("🗑️  Movie removed from cache: [%d] %s — orders blocked",
			movie.ID, movie.Title)

		// Cancel any pending orders for this movie
		cancelPendingOrders(movie.ID, movie.Title)

	default:
		log.Printf("⚠️  Unknown event type: %s — ignoring", event.Event)
	}
}

// Cancel all pending orders when a movie is deleted or out of stock
func cancelPendingOrders(movieID int, movieTitle string) {
	result, err := db.Exec(`
		UPDATE orders SET status='cancelled'
		WHERE movie_id=$1 AND status='pending'
	`, movieID)
	if err != nil {
		log.Printf("❌ Failed to cancel orders: %v", err)
		return
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("🚫 Cancelled %d pending orders for movie: %s", rows, movieTitle)
	}
}

// ═══════════════════════════════════════════════════════════
//  HTTP SERVER
// ═══════════════════════════════════════════════════════════

func startServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health",  handleHealth)
	mux.HandleFunc("/orders",  handleOrders)  // GET all, POST create
	mux.HandleFunc("/movies",  handleCachedMovies) // GET available movies (from cache)

	log.Println("🚀 Order service listening on :8081")
	log.Fatal(http.ListenAndServe(":8081", mux))
}

// ═══════════════════════════════════════════════════════════
//  HANDLERS
// ═══════════════════════════════════════════════════════════

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := db.Ping(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy"})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GET /movies → shows movies available to order (built from Kafka events)
// This proves Order Service knows about movies WITHOUT calling Inventory API
func handleCachedMovies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	rows, err := db.Query(
		"SELECT id, title, stock FROM available_movies ORDER BY id",
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var movies []CachedMovie
	for rows.Next() {
		var m CachedMovie
		rows.Scan(&m.ID, &m.Title, &m.Stock)
		movies = append(movies, m)
	}
	if movies == nil {
		movies = []CachedMovie{}
	}
	json.NewEncoder(w).Encode(movies)
}

// GET  /orders → list all orders
// POST /orders → place a new order
//
//	Validates:
//	  1. Movie exists in our Kafka cache
//	  2. Enough stock available
func handleOrders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {

	case http.MethodGet:
		rows, err := db.Query(
			"SELECT id, movie_id, customer, quantity, status FROM orders ORDER BY id",
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var orders []Order
		for rows.Next() {
			var o Order
			rows.Scan(&o.ID, &o.MovieID, &o.Customer, &o.Quantity, &o.Status)
			orders = append(orders, o)
		}
		if orders == nil {
			orders = []Order{}
		}
		json.NewEncoder(w).Encode(orders)

	case http.MethodPost:
		var o Order
		if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if o.Quantity == 0 {
			o.Quantity = 1
		}

		// ── Validate against Kafka cache ──────────────────────────────
		// We NEVER call the Inventory API — we use our local cache
		// that was built purely from Kafka events
		var cached CachedMovie
		err := db.QueryRow(
			"SELECT id, title, stock FROM available_movies WHERE id=$1",
			o.MovieID,
		).Scan(&cached.ID, &cached.Title, &cached.Stock)

		if err == sql.ErrNoRows {
			// Movie not in cache = Inventory never published it = doesn't exist
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf("movie %d not found or not available", o.MovieID),
			})
			return
		}

		if cached.Stock < o.Quantity {
			// Not enough stock
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"error": fmt.Sprintf(
					"not enough stock for '%s' — requested: %d, available: %d",
					cached.Title, o.Quantity, cached.Stock,
				),
			})
			return
		}

		// ── Save the order ────────────────────────────────────────────
		o.Status = "confirmed"
		err = db.QueryRow(`
			INSERT INTO orders (movie_id, customer, quantity, status)
			VALUES ($1, $2, $3, $4) RETURNING id
		`, o.MovieID, o.Customer, o.Quantity, o.Status).Scan(&o.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		log.Printf("✅ Order confirmed: [%d] customer=%s movie=%s qty=%d",
			o.ID, o.Customer, cached.Title, o.Quantity)

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(o)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}