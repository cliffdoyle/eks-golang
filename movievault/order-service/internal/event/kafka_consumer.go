package event

import (
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/IBM/sarama"
	"githun.com/cliffdoyle/eks-golang/order-service/internal/models"
	"githun.com/cliffdoyle/eks-golang/order-service/internal/repository"
)

type KafkaConsumer struct {
	repo repository.OrderRepository
}

func NewKafkaConsumer(repo repository.OrderRepository) *KafkaConsumer {
	return &KafkaConsumer{repo: repo}
}

func (kc *KafkaConsumer) ConsumeKafkaEvents() {
	config := sarama.NewConfig()
	config.Consumer.Offsets.Initial = sarama.OffsetOldest
	brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")

	consumer, err := sarama.NewConsumer(brokers, config)
	if err != nil {
		log.Fatal("❌ Kafka consumer failed to start:", err)
	}
	defer consumer.Close()

	partConsumer, err := consumer.ConsumePartition("movie.events", 0, sarama.OffsetOldest)
	if err != nil {
		log.Fatal("❌ Failed to consume topic movie.events:", err)
	}
	defer partConsumer.Close()

	log.Println("👂 Kafka consumer listening on topic: movie.events")

	for {
		select {
		case msg := <-partConsumer.Messages():
			log.Printf("📨 Kafka message received — offset: %d", msg.Offset)
			kc.handleKafkaEvent(msg.Value)
		case err := <-partConsumer.Errors():
			log.Printf("❌ Kafka error: %v", err)
		}
	}
}

func (kc *KafkaConsumer) handleKafkaEvent(data []byte) {
	var event models.KafkaEvent
	if err := json.Unmarshal(data, &event); err != nil {
		log.Printf("❌ Failed to parse Kafka event: %v", err)
		return
	}

	movie := event.Movie
	log.Printf("📋 Event type: %s | Movie: [%d] %s | Stock: %d",
		event.Event, movie.ID, movie.Title, movie.Stock)

	switch event.Event {
	case "movie.created":
		if err := kc.repo.UpsertCachedMovie(movie.ID, movie.Title, movie.Stock); err != nil {
			log.Printf("❌ Failed to cache movie: %v", err)
		} else {
			log.Printf("✅ Movie cached: [%d] %s (stock: %d) — orders now accepted",
				movie.ID, movie.Title, movie.Stock)
		}

	case "movie.stock_updated":
		rows, err := kc.repo.UpdateCachedMovieStock(movie.ID, movie.Stock)
		if err != nil {
			log.Printf("❌ Failed to update stock cache: %v", err)
		} else if rows > 0 {
			log.Printf("📦 Stock updated in cache: movie [%d] → %d units", movie.ID, movie.Stock)
			if movie.Stock == 0 {
				kc.repo.CancelPendingOrders(movie.ID, movie.Title)
			}
		}

	case "movie.deleted":
		kc.repo.DeleteCachedMovie(movie.ID)
		log.Printf("🗑️  Movie removed from cache: [%d] %s — orders blocked", movie.ID, movie.Title)
		kc.repo.CancelPendingOrders(movie.ID, movie.Title)

	default:
		log.Printf("⚠️  Unknown event type: %s — ignoring", event.Event)
	}
}
