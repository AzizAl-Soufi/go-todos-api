package domain

import (
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/todos/domain"
)

type User struct {
	ID        string    `json:"id" bson:"_id,omitempty"`
	Name      string    `json:"name" bson:"name"`
	Email     string    `json:"email" bson:"email"`
	CreatedAt time.Time `json:"createdAt" bson:"createdAt,omitempty"`
	IsNew     bool      `json:"-" bson:"-"`
}

type Overview struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Email string         `json:"email"`
	Todos []*domain.Todo `json:"todos"`
}