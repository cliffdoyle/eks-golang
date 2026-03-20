package repository

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/cliffdoyle/eks-golang/internal/models"
	_ "github.com/lib/pq"
)

type MovieRepository interface {
	GetMovies() ([]models.Movie, error)
	CreateMovie(m *models.Movie) error
	GetMovieByID(id int) (models.Movie, error)
	UpdateStock(id int, stock int) (models.Movie, error)
	DeleteMovie(id int) (models.Movie, error)
	Ping() error
}

type movieRepository struct {
	db *sql.DB
}

func NewMovieRepository() MovieRepository {
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
	return &movieRepository{db: db}
}

func (r *movieRepository) Ping() error {
	return r.db.Ping()
}

func (r *movieRepository) GetMovies() ([]models.Movie, error) {
	rows, err := r.db.Query("SELECT id, title, genre, year, stock FROM movies ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var movies []models.Movie
	for rows.Next() {
		var m models.Movie
		if err := rows.Scan(&m.ID, &m.Title, &m.Genre, &m.Year, &m.Stock); err != nil {
			return nil, err
		}
		movies = append(movies, m)
	}
	if movies == nil {
		movies = []models.Movie{}
	}
	return movies, nil
}

func (r *movieRepository) CreateMovie(m *models.Movie) error {
	return r.db.QueryRow(
		`INSERT INTO movies(title, genre, year, stock)
		 VALUES($1, $2, $3, $4) RETURNING id`,
		m.Title, m.Genre, m.Year, m.Stock,
	).Scan(&m.ID)
}

func (r *movieRepository) GetMovieByID(id int) (models.Movie, error) {
	var m models.Movie
	err := r.db.QueryRow(
		"SELECT id, title, genre, year, stock FROM movies WHERE id=$1", id,
	).Scan(&m.ID, &m.Title, &m.Genre, &m.Year, &m.Stock)
	return m, err
}

func (r *movieRepository) UpdateStock(id int, stock int) (models.Movie, error) {
	var m models.Movie
	err := r.db.QueryRow(
		`UPDATE movies SET stock=$1 WHERE id=$2
		 RETURNING id, title, genre, year, stock`,
		stock, id,
	).Scan(&m.ID, &m.Title, &m.Genre, &m.Year, &m.Stock)
	return m, err
}

func (r *movieRepository) DeleteMovie(id int) (models.Movie, error) {
	var m models.Movie
	err := r.db.QueryRow(
		"DELETE FROM movies WHERE id=$1 RETURNING id, title, genre, year, stock", id,
	).Scan(&m.ID, &m.Title, &m.Genre, &m.Year, &m.Stock)
	return m, err
}
