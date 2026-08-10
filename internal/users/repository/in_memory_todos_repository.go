package repository

import (
	"context"
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
	r.Mu.Lock()
	defer r.Mu.Unlock()
	if existsUser, ok := r.Data[user.Email]; ok {
		existsUser.IsNew = false
		return existsUser, nil
	}

	newUser := &domain.User{
		ID:        bson.NewObjectID(),
		Name:      user.Name,
		Email:     user.Email,
		CreatedAt: time.Now().UTC(),
		IsNew:     true,
	}
	r.Data[user.Email] = newUser
	r.Data[newUser.ID.Hex()] = newUser

	return newUser, nil
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

func (r *inMemoryUsersRepo) GetOverview(ctx context.Context, email string) (*domain.User, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	user, ok := r.Data[email]
	if !ok {
		return nil, apperrors.ErrNotFound
	}

	return user, nil
}

func (r *inMemoryUsersRepo) Delete(ctx context.Context, email string) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	if _, ok := r.Data[email]; !ok {
		return apperrors.ErrNotFound
	}

	delete(r.Data, email)

	return nil
}
