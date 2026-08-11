package domain

import (
	"time"
)

type User struct {
	ID        string    `json:"id" bson:"_id,omitempty"`
	Name      string    `json:"name" bson:"name"`
	Email     string    `json:"email" bson:"email"`
	CreatedAt time.Time `json:"createdAt" bson:"createdAt,omitempty"`
	IsNew     bool      `json:"-" bson:"-"`
}

type Overview struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Email string  `json:"email"`
	Todos []*Todo `json:"todos"`
}
