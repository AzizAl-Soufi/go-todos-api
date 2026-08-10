package service

import (
	"context"

	"github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type TodosService interface {
	CreateTodo(ctx context.Context, id bson.ObjectID, user *domain.CreateTodoDTO) (*domain.Todo, error)
	GetTodo(ctx context.Context, id bson.ObjectID, userId bson.ObjectID) (*domain.Todo, error)
	UpdateTodo(ctx context.Context, id bson.ObjectID, userId bson.ObjectID, params *domain.UpdateTodoDTO) (*domain.Todo, error)
	DeleteTodo(ctx context.Context, id bson.ObjectID, userId bson.ObjectID) error
	GetTodos(ctx context.Context, userId bson.ObjectID) ([]*domain.Todo, error)
}
