package repository

import (
	"context"

	"github.com/AzizAl-Soufi/todos-api/internal/users/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type UsersRepository interface {
	Auth(ctx context.Context, user *domain.User) (*domain.User, error)
	GetOverview(ctx context.Context, id bson.ObjectID) (*domain.User, error)
	Delete(ctx context.Context, id bson.ObjectID) error
}
