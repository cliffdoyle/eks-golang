package service

import (
	"database/sql"
	"fmt"
	"log"

	"githun.com/cliffdoyle/eks-golang/order-service/internal/models"
	"githun.com/cliffdoyle/eks-golang/order-service/internal/repository"
)

type OrderService interface {
	GetAvailableMovies() ([]models.CachedMovie, error)
	GetOrders() ([]models.Order, error)
	CreateOrder(o *models.Order) (error, int)
	PingDB() error
}

type orderService struct {
	repo repository.OrderRepository
}

func NewOrderService(repo repository.OrderRepository) OrderService {
	return &orderService{repo: repo}
}

func (s *orderService) PingDB() error {
	return s.repo.Ping()
}

func (s *orderService) GetAvailableMovies() ([]models.CachedMovie, error) {
	return s.repo.GetAvailableMovies()
}

func (s *orderService) GetOrders() ([]models.Order, error) {
	return s.repo.GetOrders()
}

func (s *orderService) CreateOrder(o *models.Order) (error, int) {
	if o.Quantity == 0 {
		o.Quantity = 1
	}

	cached, err := s.repo.GetCachedMovieByID(o.MovieID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("movie %d not found or not available", o.MovieID), 404
	} else if err != nil {
		return err, 500
	}

	if cached.Stock < o.Quantity {
		return fmt.Errorf("not enough stock for '%s' — requested: %d, available: %d",
			cached.Title, o.Quantity, cached.Stock), 409
	}

	o.Status = "confirmed"
	err = s.repo.CreateOrder(o)
	if err != nil {
		return err, 500
	}

	log.Printf("✅ Order confirmed: [%d] customer=%s movie=%s qty=%d",
		o.ID, o.Customer, cached.Title, o.Quantity)

	return nil, 201
}
