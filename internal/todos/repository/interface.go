package repository

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/todos/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type TodosRepository interface {
	Create(ctx context.Context, userId bson.ObjectID, todo *domain.Todo) error
	Update(ctx context.Context, id bson.ObjectID, userId bson.ObjectID, todo *domain.UpdateTodoDTO) error
	Delete(ctx context.Context, id bson.ObjectID, userId bson.ObjectID) error
	
	Get(ctx context.Context, id bson.ObjectID, userId bson.ObjectID) (*domain.Todo, error)
	GetAll(ctx context.Context, userId bson.ObjectID) ([]*domain.Todo, error)
	DeleteByUserID(ctx context.Context, userId bson.ObjectID) error
}
