package domain

import (
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/todos/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type User struct {
	ID        bson.ObjectID `json:"id" bson:"_id,omitempty"`
	Name      string        `json:"name" bson:"name"`
	Email     string        `json:"email" bson:"email"`
	CreatedAt time.Time     `json:"createdAt" bson:"createdAt,omitempty"`

	IsNew bool `json:"-" bson:"-"`
}

type Overview struct {
	ID    bson.ObjectID  `json:"id"`
	Name  string         `json:"name"`
	Email string         `json:"email"`
	Todos []*domain.Todo `json:"todos"`
}
