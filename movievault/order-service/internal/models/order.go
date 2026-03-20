package models

type Order struct {
	ID       int    `json:"id"`
	MovieID  int    `json:"movie_id"`
	Customer string `json:"customer"`
	Quantity int    `json:"quantity"`
	Status   string `json:"status"`
}

type CachedMovie struct {
	ID    int
	Title string
	Stock int
}

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
