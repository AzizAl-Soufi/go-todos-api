package repository

import (
	"context"
	"time"

	inmem "github.com/AzizAl-Soufi/todos-api/internal/pkg/database/in_memory"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type inMemoryTodosRepo struct {
	inmem.InMemoryClient[domain.Todo]

	// Mu   sync.RWMutex
	// data map[string]*domain.Todo
}

func NewInMemoryTodosRepository() TodosRepository {
	return &inMemoryTodosRepo{
		InMemoryClient: inmem.InMemoryClient[domain.Todo]{
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
		return ErrNotFound
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
		return nil, ErrNotFound
	}

	return todo, nil
}

func (r *inMemoryTodosRepo) DeleteByID(ctx context.Context, id bson.ObjectID) error {
	r.Mu.Lock()
	defer r.Mu.Unlock()

	todo, ok := r.Data[id.Hex()]
	if !ok {
		return ErrNotFound
	}
	delete(r.Data, todo.ID.Hex())

	return nil
}
