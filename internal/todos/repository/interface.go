package repository

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/todos/domain"
)

type TodosRepository interface {
	Create(ctx context.Context, userId string, todo *domain.Todo) error
	Update(ctx context.Context, id string, userId string, todo *domain.UpdateTodoDTO) error
	Delete(ctx context.Context, id string, userId string) error

	Get(ctx context.Context, id string, userId string) (*domain.Todo, error)
	GetAll(ctx context.Context, userId string) ([]*domain.Todo, error)
	DeleteByUserID(ctx context.Context, userId string) error
}
