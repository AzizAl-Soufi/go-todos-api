package service

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/common/middleware"
	"github.com/AzizAl-Soufi/go-todos-api/internal/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/todos/repository"
	usersDomain "github.com/AzizAl-Soufi/go-todos-api/internal/users/domain"
	usersRepository "github.com/AzizAl-Soufi/go-todos-api/internal/users/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type todoService struct {
	repo      repository.TodosRepository
	usersRepo usersRepository.UsersRepository
}

func NewTodosService(repo repository.TodosRepository, usersRepo usersRepository.UsersRepository) TodosService {
	return &todoService{repo: repo, usersRepo: usersRepo}
}

func (s *todoService) getAuthenticatedUser(ctx context.Context) (*usersDomain.User, error) {
	claims, ok := middleware.GetAuthorization(ctx)
	if !ok {
		return nil, middleware.ErrUnauthorizedContext
	}

	authenticatedUser, err := s.usersRepo.Auth(ctx, claims.ID)
	if err != nil {
		return nil, middleware.ErrUnauthorized
	}

	return authenticatedUser, nil
}

func (s *todoService) CreateTodo(ctx context.Context, todo *domain.CreateTodoDTO) (*domain.Todo, error) {

	user, err := s.getAuthenticatedUser(ctx)
	if err != nil {
		return nil, err
	}

	newTodo := &domain.Todo{
		ID:        bson.NewObjectID(),
		UserID:    user.ID,
		Title:     todo.Title,
		Completed: false,
		CreatedAt: time.Now().UTC(),
	}

	// Persist using the interface
	err = s.repo.Create(ctx, user.ID, newTodo)
	if err != nil {
		return nil, err
	}

	return newTodo, nil
}

func (s *todoService) GetTodo(ctx context.Context, id bson.ObjectID) (*domain.Todo, error) {
	user, err := s.getAuthenticatedUser(ctx)
	if err != nil {
		return nil, err
	}

	todo, err := s.repo.Get(ctx, id, user.ID)
	if err != nil {
		return nil, err
	}

	return todo, nil
}

func (s *todoService) UpdateTodo(ctx context.Context, id bson.ObjectID, params *domain.UpdateTodoDTO) (*domain.Todo, error) {
	user, err := s.getAuthenticatedUser(ctx)
	if err != nil {
		return nil, err
	}

	err = s.repo.Update(ctx, id, user.ID, params)
	if err != nil {
		return nil, err
	}

	todo, err := s.repo.Get(ctx, id, user.ID)
	if err != nil {
		return nil, err
	}

	return todo, nil
}

func (s *todoService) DeleteTodo(ctx context.Context, id bson.ObjectID) error {
	user, err := s.getAuthenticatedUser(ctx)
	if err != nil {
		return err
	}

	if err := s.repo.Delete(ctx, id, user.ID); err != nil {
		return err
	}

	return nil
}

func (s *todoService) GetTodos(ctx context.Context) ([]*domain.Todo, error) {
	user, err := s.getAuthenticatedUser(ctx)
	if err != nil {
		return nil, err
	}

	return s.repo.GetAll(ctx, user.ID)
}
