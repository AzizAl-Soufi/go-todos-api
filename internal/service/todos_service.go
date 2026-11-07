package service

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/middleware"
	"github.com/AzizAl-Soufi/go-todos-api/internal/repository/todos"
	"github.com/AzizAl-Soufi/go-todos-api/internal/repository/users"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type todoService struct {
	repo      todos.TodosRepository
	usersRepo users.UsersRepository
}

func NewTodosService(repo todos.TodosRepository, usersRepo users.UsersRepository) TodosService {
	return &todoService{repo: repo, usersRepo: usersRepo}
}

// currentUserID resolves the caller and confirms the account still exists.
func (s *todoService) currentUserID(ctx context.Context) (string, error) {
	claims, err := middleware.AuthorizationFrom(ctx)
	if err != nil {
		return "", err
	}
	user, err := s.usersRepo.Auth(ctx, claims.ID)
	if err != nil {
		return "", middleware.ErrUnauthorized
	}
	return user.ID, nil
}

func (s *todoService) CreateTodo(ctx context.Context, dto *domain.CreateTodoDTO) (*domain.Todo, error) {
	userID, err := s.currentUserID(ctx)
	if err != nil {
		return nil, err
	}

	todo := &domain.Todo{
		ID:        bson.NewObjectID().Hex(),
		UserID:    userID,
		Title:     dto.Title,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.repo.Create(ctx, userID, todo); err != nil {
		return nil, err
	}
	return todo, nil
}

func (s *todoService) GetTodo(ctx context.Context, id string) (*domain.Todo, error) {
	userID, err := s.currentUserID(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id, userID)
}

func (s *todoService) UpdateTodo(ctx context.Context, id string, params *domain.UpdateTodoDTO) (*domain.Todo, error) {
	userID, err := s.currentUserID(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, id, userID, params); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id, userID)
}

func (s *todoService) DeleteTodo(ctx context.Context, id string) error {
	userID, err := s.currentUserID(ctx)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, id, userID)
}

func (s *todoService) GetTodos(ctx context.Context) ([]*domain.Todo, error) {
	userID, err := s.currentUserID(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.GetAll(ctx, userID)
}
