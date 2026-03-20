package main

import (
	"log"
	"net/http"

	"github.com/cliffdoyle/eks-golang/internal/handler"
	"github.com/cliffdoyle/eks-golang/internal/repository"
	"github.com/cliffdoyle/eks-golang/internal/service"
)

func main() {
	log.Println("🎬 Inventory Service starting...")

	repo := repository.NewMovieRepository()
	svc := service.NewMovieService(repo)
	h := handler.NewMovieHandler(svc)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.HandleHealth)
	mux.HandleFunc("/movies", h.HandleMovies)
	mux.HandleFunc("/movies/", h.HandleMovie)

	// Wrap with CORS
	corsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		mux.ServeHTTP(w, r)
	})

	log.Println("🚀 Inventory service listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", corsHandler))
}