package repository

import (
	"context"
	"time"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/common/errors"
	inmem "github.com/AzizAl-Soufi/go-todos-api/internal/pkg/database/in_memory"
	"github.com/AzizAl-Soufi/go-todos-api/internal/users/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type inMemoryUsersRepo struct {
	inmem.InMemoryClient[bson.ObjectID, domain.User]
}

func NewInMemoryUsersRepository() UsersRepository {
	return &inMemoryUsersRepo{
		InMemoryClient: inmem.InMemoryClient[bson.ObjectID, domain.User]{
			Data: make(map[bson.ObjectID]*domain.User),
		},
	}
}
func (r *inMemoryUsersRepo) getUser(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	userData, ok := r.Data[id]
	if !ok {
		return nil, apperrors.ErrNotFound
	}

	return userData, nil
}

func (r *inMemoryUsersRepo) Register(ctx context.Context, user *domain.UserDTO) (*domain.User, error) {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	for _, existingUser := range r.Data {
		if existingUser.Email == user.Email {
			existingUser.IsNew = false
			return existingUser, nil
		}
	}

	newUser := &domain.User{
		ID:        bson.NewObjectID(),
		Name:      user.Name,
		Email:     user.Email,
		CreatedAt: time.Now().UTC(),
		IsNew:     true,
	}
	r.Data[newUser.ID] = newUser

	return newUser, nil
}

func (r *inMemoryUsersRepo) Auth(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	userData, ok := r.Data[id]
	if !ok {
		return nil, apperrors.ErrNotFound
	}

	return userData, nil
}

func (r *inMemoryUsersRepo) GetOverview(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	user, ok := r.Data[id]
	if !ok {
		return nil, apperrors.ErrNotFound
	}

	return user, nil
}

func (r *inMemoryUsersRepo) Delete(ctx context.Context, id bson.ObjectID) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	if _, ok := r.Data[id]; !ok {
		return apperrors.ErrNotFound
	}

	delete(r.Data, id)

	return nil
}
