package domain

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Todo struct {
	ID        bson.ObjectID    `json:"id" bson:"_id,omitempty"`
	Title     string    `json:"title" bson:"title"`
	Completed bool      `json:"completed" bson:"completed"`
	CreatedAt time.Time `json:"created_at" bson:"created_at,omitempty"`
}

type TodosRepository interface {
	Create(ctx context.Context, todo *Todo) error
	GetAll(ctx context.Context) ([]*Todo, error)
}

type TodosService interface {
	CreateTodo(ctx context.Context, title string) (*Todo, error)
	GetTodos(ctx context.Context) ([]*Todo, error)
}
