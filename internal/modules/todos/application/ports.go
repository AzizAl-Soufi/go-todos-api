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
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
)

// Every lookup takes a domain.Scope (the caller's personal records plus their
// teams' records), so nobody can read or change records outside it.
// Implementations return apperrors.ErrNotFound for missing (or out-of-scope)
// records and apperrors.ErrInvalidID for IDs in the wrong format. Calls made
// with a context from tx.Runner.WithinTx must join that transaction.

// ---- write side (commands) ----

type TodoRepository interface {
	// Create stores todo and assigns todo.ID.
	Create(ctx context.Context, todo *domain.Todo) error
	Get(ctx context.Context, s domain.Scope, id string) (*domain.Todo, error)
	// Update saves every field (team and creator included) of an existing
	// todo in scope.
	Update(ctx context.Context, s domain.Scope, todo *domain.Todo) error
	Delete(ctx context.Context, s domain.Scope, id string) error
	// DeleteByProject, DeleteByUser and DeleteByTeam remove matching todos;
	// none is fine. DeleteByUser removes only the user's personal todos.
	DeleteByProject(ctx context.Context, s domain.Scope, projectID string) error
	DeleteByUser(ctx context.Context, userID string) error
	DeleteByTeam(ctx context.Context, teamID string) error
	DueAlertRepository
}

// DueAlert is a todo that needs a due-date alert, and which one
// (domain.NextDueAlert).
type DueAlert struct {
	Todo *domain.Todo
	Kind domain.DueAlertKind
}

// DueAlertRepository is the system-level part of the todo store, for the
// due-date alert job. Unlike every other call it is NOT scoped: it sees every
// user's and team's todos, so only background jobs may use it, never a
// request made on behalf of a caller.
type DueAlertRepository interface {
	// ListPendingDueAlertsSystemWide returns up to limit open todos, of any
	// user or team, that need an alert at now (domain.NextDueAlert), earliest
	// deadline first (then by ID). Todos of archived projects are left out.
	ListPendingDueAlertsSystemWide(ctx context.Context, now time.Time, limit int) ([]DueAlert, error)
	// MarkDueAlerted records that the alert of kind was sent for the todo's
	// deadline dueAt. It is conditional: it reports false, changing nothing,
	// unless the todo exists, is open, still has deadline dueAt and wasn't
	// marked for it yet, so of two concurrent runs only one marks it.
	MarkDueAlerted(ctx context.Context, id string, kind domain.DueAlertKind, dueAt time.Time) (bool, error)
}

type ProjectRepository interface {
	// Create stores p and assigns p.ID.
	Create(ctx context.Context, p *domain.Project) error
	Get(ctx context.Context, s domain.Scope, id string) (*domain.Project, error)
	// Update saves every field of an existing project in scope.
	Update(ctx context.Context, s domain.Scope, p *domain.Project) error
	Delete(ctx context.Context, s domain.Scope, id string) error
	// DetachCollection moves every project of the collection out of it.
	DetachCollection(ctx context.Context, s domain.Scope, collectionID string) error
	// DeleteByUser removes only the user's personal projects.
	DeleteByUser(ctx context.Context, userID string) error
	DeleteByTeam(ctx context.Context, teamID string) error
}

type CollectionRepository interface {
	// Create stores c and assigns c.ID.
	Create(ctx context.Context, c *domain.Collection) error
	Get(ctx context.Context, s domain.Scope, id string) (*domain.Collection, error)
	// Update saves every field of an existing collection in scope.
	Update(ctx context.Context, s domain.Scope, c *domain.Collection) error
	Delete(ctx context.Context, s domain.Scope, id string) error
	// DeleteByUser removes only the user's personal collections.
	DeleteByUser(ctx context.Context, userID string) error
	DeleteByTeam(ctx context.Context, teamID string) error
}

// AuditEntry is the todos module's view of an audit event; only changes to
// team records are audited.
type AuditEntry struct {
	TeamID, ActorID      string
	Action               string // e.g. "todo.created"
	TargetType, TargetID string
	Details              map[string]string
}

// AuditLog is how commands reach the audit module.
type AuditLog interface {
	Record(ctx context.Context, e AuditEntry) error
}

// NewTeamTodo is a todo just added to a team project, as the todos module
// reports it to notifications.
type NewTeamTodo struct {
	TeamID, ProjectID, TodoID, Title string
	ActorID, ActorName               string
	ActorRole                        access.Role
}

// Notifier is how commands reach the notifications module. It is called
// inside the change's unit of work: it records that a notification is due
// (a job in the outbox), which commits or rolls back with the change, and
// the worker delivers it afterwards.
type Notifier interface {
	TodoCreated(ctx context.Context, t NewTeamTodo) error
	// TodoDue sends a due-date alert (at most once per todo, kind and
	// deadline). at is when the alert was raised, for its wording.
	TodoDue(ctx context.Context, a TodoDueAlert, at time.Time) error
}

// TodoDueAlert is a due-date alert, as the todos module reports it to
// notifications. It goes to UserID: the owner of a personal todo, the
// creator of a team todo (todos have no assignee).
type TodoDueAlert struct {
	UserID, TodoID, Title string
	TeamID, ProjectID     string // "" when personal / in the inbox
	Kind                  domain.DueAlertKind
	DueAt                 time.Time
}

// ---- read side (queries) ----

type TodoReader interface {
	Get(ctx context.Context, s domain.Scope, id string) (*domain.Todo, error)
	// List returns the todos in scope matching q, ordered by q.Sort (see
	// domain.Query).
	List(ctx context.Context, s domain.Scope, q domain.Query) ([]*domain.Todo, error)
	// Progress summarizes the todos in scope per project ID ("" is the
	// inbox); todos are overdue when open with a deadline before now.
	Progress(ctx context.Context, s domain.Scope, now time.Time) (map[string]domain.Progress, error)
}

type ProjectReader interface {
	Get(ctx context.Context, s domain.Scope, id string) (*domain.Project, error)
	// List returns the matching projects in scope, oldest first.
	List(ctx context.Context, s domain.Scope, f domain.ProjectFilter) ([]*domain.Project, error)
}

type CollectionReader interface {
	Get(ctx context.Context, s domain.Scope, id string) (*domain.Collection, error)
	// List returns the matching collections in scope, oldest first.
	List(ctx context.Context, s domain.Scope, f domain.CollectionFilter) ([]*domain.Collection, error)
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
