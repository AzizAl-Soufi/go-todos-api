package service

import (
	"context"

	"github.com/AzizAl-Soufi/todos-api/internal/users/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type UsersService interface {
	Auth(ctx context.Context, user *domain.UserDTO) (*domain.Overview, error)
	GetOverview(ctx context.Context, id bson.ObjectID) (*domain.Overview, error)
	DeleteAccount(ctx context.Context, id bson.ObjectID) error
}
