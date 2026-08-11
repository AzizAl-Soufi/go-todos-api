package users

import (
	"context"
	"time"

	inmem "github.com/AzizAl-Soufi/go-todos-api/internal/database/memory"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type inMemoryUsersRepo struct {
	inmem.MemoryClient[string, domain.User]
}

func NewInMemoryUsersRepository() UsersRepository {
	return &inMemoryUsersRepo{
		MemoryClient: inmem.MemoryClient[string, domain.User]{
			Data: make(map[string]*domain.User),
		},
	}
}
func (r *inMemoryUsersRepo) getUser(id string) (*domain.User, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	userData, ok := r.Data[id]
	if !ok {
		return nil, apperrors.ErrNotFound
	}

	return userData, nil
}

func (r *inMemoryUsersRepo) Register(_ context.Context, user *domain.UserDTO) (*domain.User, error) {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	for _, existingUser := range r.Data {
		if existingUser.Email == user.Email {
			existingUser.IsNew = false
			return existingUser, nil
		}
	}

	newUser := &domain.User{
		ID:        bson.NewObjectID().Hex(),
		Name:      user.Name,
		Email:     user.Email,
		CreatedAt: time.Now().UTC(),
		IsNew:     true,
	}
	r.Data[newUser.ID] = newUser

	return newUser, nil
}

func (r *inMemoryUsersRepo) Auth(_ context.Context, id string) (*domain.User, error) {
	return r.getUser(id)
}

func (r *inMemoryUsersRepo) GetOverview(_ context.Context, id string) (*domain.User, error) {
	return r.getUser(id)
}

func (r *inMemoryUsersRepo) Delete(_ context.Context, id string) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	u, ok := r.Data[id]
	if !ok {
		return apperrors.ErrNotFound
	}

	delete(r.Data, u.ID)
	return nil
}
