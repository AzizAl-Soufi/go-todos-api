package service

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/todos/domain"
)

type TodosService interface {
	CreateTodo(ctx context.Context, todo *domain.CreateTodoDTO) (*domain.Todo, error)
	GetTodo(ctx context.Context, id string) (*domain.Todo, error)
	UpdateTodo(ctx context.Context, id string, params *domain.UpdateTodoDTO) (*domain.Todo, error)
	DeleteTodo(ctx context.Context, id string) error
	GetTodos(ctx context.Context) ([]*domain.Todo, error)
}
