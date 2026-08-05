package repository

import (
	"context"

	"github.com/AzizAl-Soufi/todos-api/internal/pkg/mongodb"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// private struct implementing domain.TodosRepository
type mongoTodoRepo struct {
	db mongodb.MongoDBClient
}

func NewMongoTodosRepository(db mongodb.MongoDBClient) domain.TodosRepository {
	return &mongoTodoRepo{db: db}
}

func (r *mongoTodoRepo) Create(ctx context.Context, todo *domain.Todo) error {
	_, err := r.db.Collections().Todos.InsertOne(ctx, todo)
	return err
}

func (r *mongoTodoRepo) GetAll(ctx context.Context) ([]*domain.Todo, error) {
	cursor, err := r.db.Collections().Todos.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var todos []*domain.Todo
	if err := cursor.All(ctx, &todos); err != nil {
		return nil, err
	}
	if len(todos) == 0 {
		return []*domain.Todo{}, nil
	}

	return todos, nil
}
