package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/IBM/sarama"
	"github.com/cliffdoyle/eks-golang/internal/models"
	"github.com/cliffdoyle/eks-golang/internal/repository"
)

type MovieService interface {
	GetMovies() ([]models.Movie, error)
	CreateMovie(m *models.Movie) error
	GetMovieByID(id int) (models.Movie, error)
	UpdateStock(id int, stock int) (models.Movie, error)
	DeleteMovie(id int) (models.Movie, error)
	PingDB() error
}

type movieService struct {
	repo     repository.MovieRepository
	producer sarama.SyncProducer
}

func NewMovieService(repo repository.MovieRepository) MovieService {
	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Retry.Max = 5
	config.Producer.Return.Successes = true

	brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")

	producer, err := sarama.NewSyncProducer(brokers, config)
	if err != nil {
		log.Fatal("❌ Kafka producer failed:", err)
	}

	log.Println("✅ Kafka producer connected →", brokers)

	return &movieService{
		repo:     repo,
		producer: producer,
	}
}

func (s *movieService) PingDB() error {
	return s.repo.Ping()
}

func (s *movieService) publishEvent(eventType string, movie models.Movie) {
	event := models.KafkaEvent{
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
		Key:   sarama.StringEncoder(fmt.Sprintf("%d", movie.ID)),
		Value: sarama.StringEncoder(payload),
	}

	partition, offset, err := s.producer.SendMessage(msg)
	if err != nil {
		log.Printf("❌ Kafka publish failed: %v", err)
		return
	}

	log.Printf("📤 Kafka event published: event=%s movie=%s partition=%d offset=%d",
		eventType, movie.Title, partition, offset)
}

func (s *movieService) GetMovies() ([]models.Movie, error) {
	return s.repo.GetMovies()
}

func (s *movieService) CreateMovie(m *models.Movie) error {
	err := s.repo.CreateMovie(m)
	if err == nil {
		s.publishEvent("movie.created", *m)
		log.Printf("🎬 Created movie: [%d] %s (stock: %d)", m.ID, m.Title, m.Stock)
	}
	return err
}

func (s *movieService) GetMovieByID(id int) (models.Movie, error) {
	return s.repo.GetMovieByID(id)
}

func (s *movieService) UpdateStock(id int, stock int) (models.Movie, error) {
	m, err := s.repo.UpdateStock(id, stock)
	if err == nil {
		s.publishEvent("movie.stock_updated", m)
		log.Printf("📦 Stock updated: [%d] %s → stock: %d", m.ID, m.Title, m.Stock)
	}
	return m, err
}

func (s *movieService) DeleteMovie(id int) (models.Movie, error) {
	m, err := s.repo.DeleteMovie(id)
	if err == nil {
		s.publishEvent("movie.deleted", m)
		log.Printf("🗑️  Deleted movie: [%d] %s", m.ID, m.Title)
	}
	return m, err
}
