package services

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type todoService struct {
	repo domain.TodosRepository
}

func NewTodosService(repo domain.TodosRepository) domain.TodosService {
	return &todoService{repo: repo}
}

func (s *todoService) CreateTodo(ctx context.Context, title string) (*domain.Todo, error) {
	// Apply business logic (e.g., validation)
	if title == "" {
		return nil, context.Canceled
	}

	newTodo := &domain.Todo{
		ID:        bson.NewObjectID(),
		Title:     title,
		Completed: false,
		CreatedAt: time.Now(),
	}

	// Persist using the interface
	err := s.repo.Create(ctx, newTodo)
	if err != nil {
		return nil, err
	}

	return newTodo, nil
}

func (s *todoService) GetTodos(ctx context.Context) ([]*domain.Todo, error) {
	return s.repo.GetAll(ctx)
}
