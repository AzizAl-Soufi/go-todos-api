package repository

import (
	"context"
	"errors"
	"time"

	apperrors "github.com/AzizAl-Soufi/todos-api/internal/common/errors"
	inmem "github.com/AzizAl-Soufi/todos-api/internal/pkg/database/in_memory"
	"github.com/AzizAl-Soufi/todos-api/internal/users/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type inMemoryUsersRepo struct {
	inmem.InMemoryClient[string, domain.User]
}

func NewInMemoryUsersRepository() UsersRepository {
	return &inMemoryUsersRepo{
		InMemoryClient: inmem.InMemoryClient[string, domain.User]{
			Data: make(map[string]*domain.User),
		},
	}
}

func (r *inMemoryUsersRepo) Register(ctx context.Context, user *domain.UserDTO) (*domain.User, error) {
	var userData *domain.User
	r.Mu.Lock()
	defer r.Mu.Unlock()

	existsUser, err := r.Auth(ctx, user.Email)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			newUser := &domain.User{
				ID:        bson.NewObjectID(),
				Name:      existsUser.Name,
				Email:     user.Email,
				CreatedAt: time.Now().UTC(),
				IsNew:     true,
			}
			userData = newUser
			return userData, nil
		} else {
			return nil, err
		}
	}
	userData = existsUser
	userData.IsNew = false

	return userData, nil
}

func (r *inMemoryUsersRepo) Auth(ctx context.Context, email string) (*domain.User, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	userData, ok := r.Data[email]
	if !ok {
		return nil, apperrors.ErrNotFound
	}

	return userData, nil
}

func (r *inMemoryUsersRepo) GetOverview(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	user, ok := r.Data[id.Hex()]
	if !ok {
		return nil, apperrors.ErrNotFound
	}

	return user, nil
}

func (r *inMemoryUsersRepo) Delete(ctx context.Context, id bson.ObjectID) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	// Just check if it exists using the ID passed into the function
	if _, ok := r.Data[id.Hex()]; !ok {
		return apperrors.ErrNotFound
	}

	// Delete using the hex string directly
	delete(r.Data, id.Hex())

	return nil
}
