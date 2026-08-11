package service

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/middleware"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
)

type UsersService interface {
	Register(ctx context.Context, user *domain.UserDTO) (*domain.RegisterUserResponse, error)
	Auth(ctx context.Context) (*domain.Overview, error)
	RefreshToken(ctx context.Context, params *domain.RefreshRequest) (*middleware.TokenPair, error)
	GetOverview(ctx context.Context) (*domain.Overview, error)
	DeleteAccount(ctx context.Context) error
}

type TodosService interface {
	CreateTodo(ctx context.Context, todo *domain.CreateTodoDTO) (*domain.Todo, error)
	GetTodo(ctx context.Context, id string) (*domain.Todo, error)
	UpdateTodo(ctx context.Context, id string, params *domain.UpdateTodoDTO) (*domain.Todo, error)
	DeleteTodo(ctx context.Context, id string) error
	GetTodos(ctx context.Context) ([]*domain.Todo, error)
}
