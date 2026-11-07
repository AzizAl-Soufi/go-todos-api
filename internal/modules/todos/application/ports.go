// Package application holds the todos module's ports. The use cases live in
// its two subpackages, following CQRS:
//
//	command/  writes: create, update and delete todos, projects, collections
//	query/    reads: todos with filters and sorting, projects and collections
//	          with their progress
//
// Commands depend on the repositories, queries only on the read-only readers.
package application

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
)

// Every lookup is scoped to the owner, so one user can never read or change
// another user's data. Implementations return apperrors.ErrNotFound for
// missing (or foreign) records and apperrors.ErrInvalidID for IDs in the wrong
// format. Calls made with a context from tx.Runner.WithinTx must join that
// transaction.

// ---- write side (commands) ----

type TodoRepository interface {
	// Create stores todo and assigns todo.ID.
	Create(ctx context.Context, todo *domain.Todo) error
	Get(ctx context.Context, userID, id string) (*domain.Todo, error)
	// Update saves every field of an existing todo owned by todo.UserID.
	Update(ctx context.Context, todo *domain.Todo) error
	Delete(ctx context.Context, userID, id string) error
	// DeleteByProject and DeleteByUser remove matching todos; none is fine.
	DeleteByProject(ctx context.Context, userID, projectID string) error
	DeleteByUser(ctx context.Context, userID string) error
}

type ProjectRepository interface {
	// Create stores p and assigns p.ID.
	Create(ctx context.Context, p *domain.Project) error
	Get(ctx context.Context, userID, id string) (*domain.Project, error)
	// Update saves every field of an existing project owned by p.UserID.
	Update(ctx context.Context, p *domain.Project) error
	Delete(ctx context.Context, userID, id string) error
	// DetachCollection moves every project of the collection out of it.
	DetachCollection(ctx context.Context, userID, collectionID string) error
	DeleteByUser(ctx context.Context, userID string) error
}

type CollectionRepository interface {
	// Create stores c and assigns c.ID.
	Create(ctx context.Context, c *domain.Collection) error
	Get(ctx context.Context, userID, id string) (*domain.Collection, error)
	// Update saves every field of an existing collection owned by c.UserID.
	Update(ctx context.Context, c *domain.Collection) error
	Delete(ctx context.Context, userID, id string) error
	DeleteByUser(ctx context.Context, userID string) error
}

// ---- read side (queries) ----

type TodoReader interface {
	Get(ctx context.Context, userID, id string) (*domain.Todo, error)
	// List returns the todos matching q, ordered by q.Sort (see domain.Query).
	List(ctx context.Context, userID string, q domain.Query) ([]*domain.Todo, error)
	// Progress summarizes the user's todos per project ID ("" is the inbox);
	// todos are overdue when open with a deadline before now.
	Progress(ctx context.Context, userID string, now time.Time) (map[string]domain.Progress, error)
}

type ProjectReader interface {
	Get(ctx context.Context, userID, id string) (*domain.Project, error)
	// List returns the matching projects, oldest first.
	List(ctx context.Context, userID string, f domain.ProjectFilter) ([]*domain.Project, error)
}

type CollectionReader interface {
	Get(ctx context.Context, userID, id string) (*domain.Collection, error)
	// List returns the matching collections, oldest first.
	List(ctx context.Context, userID string, f domain.ArchiveFilter) ([]*domain.Collection, error)
}

// ---- adapters serve both sides ----

type (
	TodoStore interface {
		TodoRepository
		TodoReader
	}
	ProjectStore interface {
		ProjectRepository
		ProjectReader
	}
	CollectionStore interface {
		CollectionRepository
		CollectionReader
	}
)
