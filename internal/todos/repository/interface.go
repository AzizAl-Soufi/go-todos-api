package repository

import (
	"context"

	"github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type TodosRepository interface {
	Create(ctx context.Context, todo *domain.Todo) error
	Update(ctx context.Context, id bson.ObjectID, todo *domain.UpdateTodoDTO) error
	DeleteByID(ctx context.Context, id bson.ObjectID) error
	GetByID(ctx context.Context, id bson.ObjectID) (*domain.Todo, error)
	GetAll(ctx context.Context) ([]*domain.Todo, error)
}
