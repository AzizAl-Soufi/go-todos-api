package repository

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/users/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type UsersRepository interface {
	Register(ctx context.Context, user *domain.UserDTO) (*domain.User, error)
	Auth(ctx context.Context, id bson.ObjectID) (*domain.User, error)
	GetOverview(ctx context.Context, id bson.ObjectID) (*domain.User, error)
	Delete(ctx context.Context, id bson.ObjectID) error
}
