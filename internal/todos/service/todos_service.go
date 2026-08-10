package service

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/todos-api/internal/common/middleware"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/repository"
	usersRepository "github.com/AzizAl-Soufi/todos-api/internal/users/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type todoService struct {
	repo      repository.TodosRepository
	usersRepo usersRepository.UsersRepository
}

func NewTodosService(repo repository.TodosRepository, usersRepo usersRepository.UsersRepository) TodosService {
	return &todoService{repo: repo, usersRepo: usersRepo}
}

func (s *todoService) CreateTodo(ctx context.Context, todo *domain.CreateTodoDTO) (*domain.Todo, error) {
	claims, ok := middleware.GetAuthorization(ctx)
	if !ok {
		return nil, middleware.ErrUnauthorizedContext
	}

	authenticatedUser, err := s.usersRepo.Auth(ctx, claims.Email)
	if err != nil {
		return nil, middleware.ErrUnauthorized
	}

	newTodo := &domain.Todo{
		ID:        bson.NewObjectID(),
		UserID:    authenticatedUser.ID,
		Title:     todo.Title,
		Completed: false,
		CreatedAt: time.Now(),
	}

	// Persist using the interface
	err = s.repo.Create(ctx, authenticatedUser.ID, newTodo)
	if err != nil {
		return nil, err
	}

	return newTodo, nil
}

func (s *todoService) GetTodo(ctx context.Context, id bson.ObjectID) (*domain.Todo, error) {
	claims, ok := middleware.GetAuthorization(ctx)
	if !ok {
		return nil, middleware.ErrUnauthorizedContext
	}

	authenticatedUser, err := s.usersRepo.Auth(ctx, claims.Email)
	if err != nil {
		return nil, middleware.ErrUnauthorized
	}

	todo, err := s.repo.Get(ctx, id, authenticatedUser.ID)
	if err != nil {
		return nil, err
	}

	return todo, nil
}

func (s *todoService) UpdateTodo(ctx context.Context, id bson.ObjectID, params *domain.UpdateTodoDTO) (*domain.Todo, error) {
	claims, ok := middleware.GetAuthorization(ctx)
	if !ok {
		return nil, middleware.ErrUnauthorizedContext
	}

	authenticatedUser, err := s.usersRepo.Auth(ctx, claims.Email)
	if err != nil {
		return nil, middleware.ErrUnauthorized
	}

	err = s.repo.Update(ctx, id, authenticatedUser.ID, params)
	if err != nil {
		return nil, err
	}

	todo, err := s.repo.Get(ctx, id, authenticatedUser.ID)
	if err != nil {
		return nil, err
	}

	return todo, nil
}

func (s *todoService) DeleteTodo(ctx context.Context, id bson.ObjectID) error {
	claims, ok := middleware.GetAuthorization(ctx)
	if !ok {
		return middleware.ErrUnauthorizedContext
	}

	authenticatedUser, err := s.usersRepo.Auth(ctx, claims.Email)
	if err != nil {
		return middleware.ErrUnauthorized
	}

	if err := s.repo.DeleteTodo(ctx, id, authenticatedUser.ID); err != nil {
		return err
	}

	return nil
}

func (s *todoService) GetTodos(ctx context.Context) ([]*domain.Todo, error) {
	claims, ok := middleware.GetAuthorization(ctx)
	if !ok {
		return nil, middleware.ErrUnauthorizedContext
	}

	authenticatedUser, err := s.usersRepo.Auth(ctx, claims.Email)
	if err != nil {
		return nil, middleware.ErrUnauthorized
	}

	return s.repo.GetAll(ctx, authenticatedUser.ID)
}
