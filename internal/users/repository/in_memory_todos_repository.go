package repository

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/todos-api/internal/common"
	inmem "github.com/AzizAl-Soufi/todos-api/internal/pkg/database/in_memory"
	"github.com/AzizAl-Soufi/todos-api/internal/users/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Ensure ErrNotFound is defined in this package, e.g., var ErrNotFound = errors.New("not found")

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

func (r *inMemoryUsersRepo) Auth(ctx context.Context, user *domain.User) (*domain.User, error) {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	if user.ID == (bson.ObjectID{}) {
		user.ID = bson.NewObjectID()
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}

	userData, ok := r.Data[user.Email]
	if ok {
		r.Data[user.Email] = user
		return user, nil
	}

	return userData, nil
}

func (r *inMemoryUsersRepo) GetOverview(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	user, ok := r.Data[id.Hex()]
	if !ok {
		return nil, common.ErrNotFound
	}

	return user, nil
}

func (r *inMemoryUsersRepo) Delete(ctx context.Context, id bson.ObjectID) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	// Just check if it exists using the ID passed into the function
	if _, ok := r.Data[id.Hex()]; !ok {
		return common.ErrNotFound
	}

	// Delete using the hex string directly
	delete(r.Data, id.Hex())

	return nil
}
