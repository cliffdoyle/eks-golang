package repository

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
	"githun.com/cliffdoyle/eks-golang/order-service/internal/models"
)

type OrderRepository interface {
	GetAvailableMovies() ([]models.CachedMovie, error)
	GetCachedMovieByID(id int) (models.CachedMovie, error)
	GetOrders() ([]models.Order, error)
	CreateOrder(o *models.Order) error
	UpsertCachedMovie(id int, title string, stock int) error
	UpdateCachedMovieStock(id int, stock int) (int64, error)
	DeleteCachedMovie(id int) error
	CancelPendingOrders(movieID int, movieTitle string) error
	Ping() error
}

type orderRepository struct {
	db *sql.DB
}

func NewOrderRepository() OrderRepository {
	dsn := fmt.Sprintf(
		"host=%s port=5432 user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"),
	)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal("❌ DB connection failed:", err)
	}
	if err = db.Ping(); err != nil {
		log.Fatal("❌ DB ping failed:", err)
	}

	db.Exec(`
		CREATE TABLE IF NOT EXISTS orders (
			id       SERIAL PRIMARY KEY,
			movie_id INT NOT NULL,
			customer TEXT NOT NULL,
			quantity INT DEFAULT 1,
			status   TEXT DEFAULT 'pending'
		)
	`)

	db.Exec(`
		CREATE TABLE IF NOT EXISTS available_movies (
			id    INT PRIMARY KEY,
			title TEXT,
			stock INT DEFAULT 0
		)
	`)

	log.Println("✅ PostgreSQL connected — orders + available_movies tables ready")
	return &orderRepository{db: db}
}

func (r *orderRepository) Ping() error {
	return r.db.Ping()
}

func (r *orderRepository) GetAvailableMovies() ([]models.CachedMovie, error) {
	rows, err := r.db.Query("SELECT id, title, stock FROM available_movies ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var movies []models.CachedMovie
	for rows.Next() {
		var m models.CachedMovie
		rows.Scan(&m.ID, &m.Title, &m.Stock)
		movies = append(movies, m)
	}
	if movies == nil {
		movies = []models.CachedMovie{}
	}
	return movies, nil
}

func (r *orderRepository) GetCachedMovieByID(id int) (models.CachedMovie, error) {
	var m models.CachedMovie
	err := r.db.QueryRow(
		"SELECT id, title, stock FROM available_movies WHERE id=$1", id,
	).Scan(&m.ID, &m.Title, &m.Stock)
	return m, err
}

func (r *orderRepository) GetOrders() ([]models.Order, error) {
	rows, err := r.db.Query("SELECT id, movie_id, customer, quantity, status FROM orders ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []models.Order
	for rows.Next() {
		var o models.Order
		rows.Scan(&o.ID, &o.MovieID, &o.Customer, &o.Quantity, &o.Status)
		orders = append(orders, o)
	}
	if orders == nil {
		orders = []models.Order{}
	}
	return orders, nil
}

func (r *orderRepository) CreateOrder(o *models.Order) error {
	return r.db.QueryRow(`
		INSERT INTO orders (movie_id, customer, quantity, status)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, o.MovieID, o.Customer, o.Quantity, o.Status).Scan(&o.ID)
}

func (r *orderRepository) UpsertCachedMovie(id int, title string, stock int) error {
	_, err := r.db.Exec(`
		INSERT INTO available_movies (id, title, stock)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE
		SET title = $2, stock = $3
	`, id, title, stock)
	return err
}

func (r *orderRepository) UpdateCachedMovieStock(id int, stock int) (int64, error) {
	result, err := r.db.Exec("UPDATE available_movies SET stock=$1 WHERE id=$2", stock, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *orderRepository) DeleteCachedMovie(id int) error {
	_, err := r.db.Exec("DELETE FROM available_movies WHERE id=$1", id)
	return err
}

func (r *orderRepository) CancelPendingOrders(movieID int, movieTitle string) error {
	result, err := r.db.Exec(`
		UPDATE orders SET status='cancelled'
		WHERE movie_id=$1 AND status='pending'
	`, movieID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("🚫 Cancelled %d pending orders for movie: %s", rows, movieTitle)
	}
	return nil
}
