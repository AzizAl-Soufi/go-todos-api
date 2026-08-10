package repository

import (
	"context"

	"github.com/AzizAl-Soufi/todos-api/internal/users/domain"
)

type UsersRepository interface {
	Register(ctx context.Context, user *domain.UserDTO) (*domain.User, error)
	Auth(ctx context.Context, email string) (*domain.User, error)
	GetOverview(ctx context.Context, email string) (*domain.User, error)
	Delete(ctx context.Context, email string) error
}
