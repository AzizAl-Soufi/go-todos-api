package todos

import (
	"context"
	"time"

	inmem "github.com/AzizAl-Soufi/go-todos-api/internal/database/memory"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type inMemoryTodosRepo struct {
	inmem.MemoryClient[string, domain.Todo]
}

func NewInMemoryTodosRepository() TodosRepository {
	return &inMemoryTodosRepo{
		MemoryClient: inmem.MemoryClient[string, domain.Todo]{
			Data: make(map[string]*domain.Todo),
		},
	}
}

func (r *inMemoryTodosRepo) Create(_ context.Context, userID string, todo *domain.Todo) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	stored := inmem.Clone(todo)
	stored.UserID = userID
	if stored.ID == "" {
		stored.ID = bson.NewObjectID().Hex()
	}
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now()
	}
	key := stored.ID
	if _, exists := r.Data[key]; exists {
		return apperrors.ErrDuplicate
	}

	r.Data[key] = stored
	return nil
}

func (r *inMemoryTodosRepo) Update(ctx context.Context, id string, userID string, todo *domain.UpdateTodoDTO) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	exists, ok := r.Data[id]
	if !ok || exists.UserID != userID {
		return apperrors.ErrNotFound
	}

	updated, err := todo.UpdateEntity(exists)
	if err != nil {
		return err
	}

	r.Data[id] = updated
	return nil
}

func (r *inMemoryTodosRepo) GetByID(_ context.Context, id string) (*domain.Todo, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	todo, ok := r.Data[id]
	if !ok {
		return nil, apperrors.ErrNotFound
	}

	return inmem.Clone(todo), nil
}

func (r *inMemoryTodosRepo) Get(_ context.Context, id string, userID string) (*domain.Todo, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	todo, ok := r.Data[id]
	if !ok || todo.UserID != userID {
		return nil, apperrors.ErrNotFound
	}

	return inmem.Clone(todo), nil
}

func (r *inMemoryTodosRepo) Delete(_ context.Context, todoId string, userId string) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	todo, ok := r.Data[todoId]
	if !ok {
		return apperrors.ErrNotFound
	}

	if todo.UserID != userId {
		return apperrors.ErrNotFound
	}

	delete(r.Data, todoId)

	return nil
}

func (r *inMemoryTodosRepo) GetAll(_ context.Context, userID string) ([]*domain.Todo, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()
	todos := make([]*domain.Todo, 0, len(r.Data))
	for _, t := range r.Data {
		if t.UserID == userID {
			todos = append(todos, inmem.Clone(t))
		}
	}
	return todos, nil
}

func (r *inMemoryTodosRepo) DeleteByUserID(_ context.Context, userId string) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	for key, todo := range r.Data {
		if todo.UserID == userId {
			delete(r.Data, key)
		}
	}

	return nil
}
