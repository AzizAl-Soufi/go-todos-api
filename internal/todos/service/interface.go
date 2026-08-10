package service

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/todos/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type TodosService interface {
	CreateTodo(ctx context.Context, todo *domain.CreateTodoDTO) (*domain.Todo, error)
	GetTodo(ctx context.Context, id bson.ObjectID) (*domain.Todo, error)
	UpdateTodo(ctx context.Context, id bson.ObjectID, params *domain.UpdateTodoDTO) (*domain.Todo, error)
	DeleteTodo(ctx context.Context, id bson.ObjectID) error
	GetTodos(ctx context.Context) ([]*domain.Todo, error)
}
