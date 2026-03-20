package main

import (
	"log"
	"net/http"

	"githun.com/cliffdoyle/eks-golang/order-service/internal/event"
	"githun.com/cliffdoyle/eks-golang/order-service/internal/handler"
	"githun.com/cliffdoyle/eks-golang/order-service/internal/repository"
	"githun.com/cliffdoyle/eks-golang/order-service/internal/service"
)

func main() {
	log.Println("🛒 Order Service starting...")

	repo := repository.NewOrderRepository()
	kafkaConsumer := event.NewKafkaConsumer(repo)
	svc := service.NewOrderService(repo)
	h := handler.NewOrderHandler(svc)

	go kafkaConsumer.ConsumeKafkaEvents()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.HandleHealth)
	mux.HandleFunc("/orders", h.HandleOrders)
	mux.HandleFunc("/movies", h.HandleCachedMovies)

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

	log.Println("🚀 Order service listening on :8081")
	log.Fatal(http.ListenAndServe(":8081", corsHandler))
}