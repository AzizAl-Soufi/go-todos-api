package repository

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/todos-api/internal/common"
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

func (r *inMemoryTodosRepo) Create(ctx context.Context, todo *domain.Todo) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	if todo.ID == (bson.ObjectID{}) {
		todo.ID = bson.NewObjectID()
	}
	if todo.CreatedAt.IsZero() {
		todo.CreatedAt = time.Now()
	}
	r.Data[todo.ID.Hex()] = todo
	return nil
}

func (r *inMemoryTodosRepo) Update(ctx context.Context, id bson.ObjectID, todo *domain.UpdateTodoDTO) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	exists, ok := r.Data[id.Hex()]
	if !ok {
		return common.ErrNotFound
	}

	updated, err := todo.UpdateEntity(exists)
	if err != nil {
		return err
	}

	r.Data[id.Hex()] = updated
	return nil
}

func (r *inMemoryTodosRepo) GetAll(ctx context.Context) ([]*domain.Todo, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()
	todos := make([]*domain.Todo, 0, len(r.Data))
	for _, t := range r.Data {
		todos = append(todos, t)
	}
	return todos, nil
}

func (r *inMemoryTodosRepo) GetByID(ctx context.Context, id bson.ObjectID) (*domain.Todo, error) {
	r.Mu.RLock()
	defer r.Mu.RUnlock()

	todo, ok := r.Data[id.Hex()]
	if !ok {
		return nil, common.ErrNotFound
	}

	return todo, nil
}

func (r *inMemoryTodosRepo) DeleteByID(ctx context.Context, id bson.ObjectID) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	todo, ok := r.Data[id.Hex()]
	if !ok {
		return common.ErrNotFound
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

func (r *inMemoryTodosRepo) DeleteTodoByUserID(ctx context.Context, userId bson.ObjectID, todoId bson.ObjectID) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	todo, ok := r.Data[todoId.Hex()]
	if !ok {
		return common.ErrNotFound
	}

	if todo.UserID != userId {
		return common.ErrNotFound
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
