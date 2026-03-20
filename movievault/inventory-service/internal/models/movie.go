package models

type Movie struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Genre string `json:"genre"`
	Year  int    `json:"year"`
	Stock int    `json:"stock"`
}

type KafkaEvent struct {
	Event string `json:"event"`
	Movie Movie  `json:"movie"`
}
