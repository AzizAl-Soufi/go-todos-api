package service

import (
	"context"

	"github.com/AzizAl-Soufi/todos-api/internal/common/middleware"
	"github.com/AzizAl-Soufi/todos-api/internal/users/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type UsersService interface {
	Register(ctx context.Context, user *domain.UserDTO) (*domain.RegisterUserResponse, error)
	Auth(ctx context.Context, auth *middleware.Authorization) (*domain.Overview, error)
	RefreshToken(ctx context.Context, params *domain.RefreshRequest) (*middleware.TokenPair, error)
	GetOverview(ctx context.Context, id bson.ObjectID) (*domain.Overview, error)
	DeleteAccount(ctx context.Context, id bson.ObjectID) error
}
