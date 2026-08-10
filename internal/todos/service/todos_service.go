package service

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type todoService struct {
	repo repository.TodosRepository
}

func NewTodosService(repo repository.TodosRepository) TodosService {
	return &todoService{repo: repo}
}

func (s *todoService) CreateTodo(ctx context.Context, userId bson.ObjectID, user *domain.CreateTodoDTO) (*domain.Todo, error) {

	newTodo := &domain.Todo{
		ID:        bson.NewObjectID(),
		UserID:    userId,
		Title:     user.Title,
		Completed: false,
		CreatedAt: time.Now(),
	}

	// Persist using the interface
	err := s.repo.Create(ctx, userId, newTodo)
	if err != nil {
		return nil, err
	}

	return newTodo, nil
}

func (s *todoService) GetTodo(ctx context.Context, id bson.ObjectID, userId bson.ObjectID) (*domain.Todo, error) {
	todo, err := s.repo.Get(ctx, id, userId)
	if err != nil {
		return nil, err
	}

	return todo, nil
}

func (s *todoService) UpdateTodo(ctx context.Context, id bson.ObjectID, userId bson.ObjectID, params *domain.UpdateTodoDTO) (*domain.Todo, error) {
	err := s.repo.Update(ctx, id, userId, params)
	if err != nil {
		return nil, err
	}

	todo, err := s.repo.Get(ctx, id, userId)
	if err != nil {
		return nil, err
	}

	return todo, nil
}

func (s *todoService) DeleteTodo(ctx context.Context, id bson.ObjectID, userId bson.ObjectID) error {
	if err := s.repo.DeleteTodo(ctx, id, userId); err != nil {
		return err
	}

	return nil
}


func (s *todoService) GetTodos(ctx context.Context, userId bson.ObjectID) ([]*domain.Todo, error) {
	return s.repo.GetAll(ctx, userId)
}
