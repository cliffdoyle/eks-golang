package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/cliffdoyle/eks-golang/internal/models"
	"github.com/cliffdoyle/eks-golang/internal/service"
)

type MovieHandler struct {
	service service.MovieService
}

func NewMovieHandler(s service.MovieService) *MovieHandler {
	return &MovieHandler{service: s}
}

func (h *MovieHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if err := h.service.PingDB(); err != nil {
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

func (h *MovieHandler) HandleMovies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {

	case http.MethodGet:
		movies, err := h.service.GetMovies()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(movies)

	case http.MethodPost:
		var m models.Movie
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		err := h.service.CreateMovie(&m)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(m)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *MovieHandler) HandleMovie(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/movies/"))
	if err != nil {
		http.Error(w, "invalid movie id", http.StatusBadRequest)
		return
	}

	switch r.Method {

	case http.MethodGet:
		m, err := h.service.GetMovieByID(id)
		if err == sql.ErrNoRows {
			http.Error(w, "movie not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(m)

	case http.MethodPut:
		var body struct {
			Stock int `json:"stock"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		m, err := h.service.UpdateStock(id, body.Stock)
		if err == sql.ErrNoRows {
			http.Error(w, "movie not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(m)

	case http.MethodDelete:
		_, err := h.service.DeleteMovie(id)
		if err == sql.ErrNoRows {
			http.Error(w, "movie not found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
