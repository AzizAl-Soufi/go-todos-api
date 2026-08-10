package repository

import (
	"context"
	"time"

	apperrors "github.com/AzizAl-Soufi/todos-api/internal/common/errors"
	inmem "github.com/AzizAl-Soufi/todos-api/internal/pkg/database/in_memory"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type inMemoryTodosRepo struct {
	inmem.InMemoryClient[string, domain.Todo]
}

func NewInMemoryTodosRepository() TodosRepository {
	return &inMemoryTodosRepo{
		InMemoryClient: inmem.InMemoryClient[string, domain.Todo]{
			Data: make(map[string]*domain.Todo),
		},
	}
}

func (r *inMemoryTodosRepo) Create(ctx context.Context, userID bson.ObjectID, todo *domain.Todo) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	todo.UserID = userID
	if todo.ID == (bson.ObjectID{}) {
		todo.ID = bson.NewObjectID()
	}
	if todo.CreatedAt.IsZero() {
		todo.CreatedAt = time.Now()
	}
	r.Data[todo.ID.Hex()] = todo
	return nil
}

func (r *inMemoryTodosRepo) Update(ctx context.Context, id bson.ObjectID, userID bson.ObjectID, todo *domain.UpdateTodoDTO) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	exists, ok := r.Data[id.Hex()]
	if !ok || exists.UserID != userID {
		return apperrors.ErrNotFound
	}

	updated, err := todo.UpdateEntity(exists)
	if err != nil {
		return err
	}

	r.Data[id.Hex()] = updated
	return nil
}

func (r *inMemoryTodosRepo) GetAll(ctx context.Context, userID bson.ObjectID) ([]*domain.Todo, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()
	todos := make([]*domain.Todo, 0, len(r.Data))
	for _, t := range r.Data {
		if t.UserID == userID {
			todos = append(todos, t)
		}
	}
	return todos, nil
}

func (r *inMemoryTodosRepo) GetByID(ctx context.Context, id bson.ObjectID) (*domain.Todo, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	todo, ok := r.Data[id.Hex()]
	if !ok {
		return nil, apperrors.ErrNotFound
	}

	return todo, nil
}

func (r *inMemoryTodosRepo) Get(ctx context.Context, id bson.ObjectID, userID bson.ObjectID) (*domain.Todo, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	todo, ok := r.Data[id.Hex()]
	if !ok || todo.UserID != userID {
		return nil, apperrors.ErrNotFound
	}

	return todo, nil
}

func (r *inMemoryTodosRepo) DeleteByID(ctx context.Context, id bson.ObjectID) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	todo, ok := r.Data[id.Hex()]
	if !ok {
		return apperrors.ErrNotFound
	}
	delete(r.Data, todo.ID.Hex())

	return nil
}

func (r *inMemoryTodosRepo) GetByUserID(ctx context.Context, id bson.ObjectID) ([]*domain.Todo, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	todos := make([]*domain.Todo, 0)
	for _, t := range r.Data {
		if t.UserID == id {
			todos = append(todos, t)
		}
	}
	return todos, nil
}

func (r *inMemoryTodosRepo) DeleteTodo(ctx context.Context, todoId bson.ObjectID, userId bson.ObjectID) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	todo, ok := r.Data[todoId.Hex()]
	if !ok {
		return apperrors.ErrNotFound
	}

	if todo.UserID != userId {
		return apperrors.ErrNotFound
	}

	delete(r.Data, todoId.Hex())

	return nil
}

func (r *inMemoryTodosRepo) DeleteByUserID(ctx context.Context, userId bson.ObjectID) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	for key, todo := range r.Data {
		if todo.UserID == userId {
			delete(r.Data, key)
		}
	}

	return nil
}
