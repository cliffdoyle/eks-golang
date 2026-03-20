package handler

import (
	"encoding/json"
	"net/http"

	"githun.com/cliffdoyle/eks-golang/order-service/internal/models"
	"githun.com/cliffdoyle/eks-golang/order-service/internal/service"
)

type OrderHandler struct {
	service service.OrderService
}

func NewOrderHandler(s service.OrderService) *OrderHandler {
	return &OrderHandler{service: s}
}

func (h *OrderHandler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if err := h.service.PingDB(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy"})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *OrderHandler) HandleCachedMovies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	movies, err := h.service.GetAvailableMovies()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(movies)
}

func (h *OrderHandler) HandleOrders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		orders, err := h.service.GetOrders()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(orders)

	case http.MethodPost:
		var o models.Order
		if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}

		err, status := h.service.CreateOrder(&o)
		if err != nil {
			w.WriteHeader(status)
			if status == 404 || status == 409 {
				json.NewEncoder(w).Encode(map[string]string{
					"error": err.Error(),
				})
			} else {
				http.Error(w, err.Error(), status)
			}
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(o)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
