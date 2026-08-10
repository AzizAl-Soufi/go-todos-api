package common

import (
	"context"
)

type ListOptions interface {
	Validate(T any) error
}

// Repository is a generic interface for basic CRUD operations.
// Type parameter T represents the entity type, and ID represents the identifier type.
type Repository[T any, ID any] interface {
	// Create inserts a new entity into the repository.
	Create(ctx context.Context, entity *T) error

	// GetByID retrieves an entity by its unique identifier.
	GetByID(ctx context.Context, id ID) (*T, error)

	// Update modifies an existing entity in the repository.
	Update(ctx context.Context, entity *T) error

	// Delete removes an entity from the repository by its identifier.
	Delete(ctx context.Context, id ID) error

	// List retrieves all entities with optional pagination.
	List(ctx context.Context, query ListOptions) ([]*T, error)

	// Count returns the total number of entities in the repository.
	Count(ctx context.Context) (int64, error)
}
