package repository

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/users/domain"
)

type UsersRepository interface {
	Register(ctx context.Context, user *domain.UserDTO) (*domain.User, error)
	Auth(ctx context.Context, id string) (*domain.User, error)
	GetOverview(ctx context.Context, id string) (*domain.User, error)
	Delete(ctx context.Context, id string) error
}
