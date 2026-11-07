// Package todos is the todos module's public surface: todos organized in
// projects (lists) and collections (groups of projects), each either personal
// or owned by a team. It covers construction, HTTP routes and the API other
// modules may call. Team memberships come from platform/access (the caller's
// Actor), so this module never imports the teams module.
//
// Layout (CQRS inside hexagonal layers):
//
//	domain/                entities, invariants, query and progress semantics
//	application/           ports
//	application/command/   write use cases, one handler per command
//	application/query/     read use cases, one handler per query
//	transport/             driving adapter (HTTP)
//	adapters/              driven adapters (persistence, audit and notifications gateways)
package todos

import (
	"context"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/transport/httpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

// Stores are the persistence adapters; each serves commands and queries.
type (
	TodoStore       = application.TodoStore
	ProjectStore    = application.ProjectStore
	CollectionStore = application.CollectionStore
	// AuditLog receives the changes to team records; AuditEntry is one change.
	AuditLog   = application.AuditLog
	AuditEntry = application.AuditEntry
	// Notifier hears about new team todos; NewTeamTodo is one.
	Notifier    = application.Notifier
	NewTeamTodo = application.NewTeamTodo
)

// Deps are the adapters the module runs on.
type Deps struct {
	Todos       TodoStore
	Projects    ProjectStore
	Collections CollectionStore
	Audit       AuditLog // nil records nothing
	Notifier    Notifier // nil notifies nobody
	Tx          tx.Runner
	Now         func() time.Time // nil means time.Now
}

type Module struct {
	Commands command.Handlers
	Queries  query.Handlers
}

func New(d Deps) *Module {
	return &Module{
		Commands: command.New(command.Deps{
			Todos:       d.Todos,
			Projects:    d.Projects,
			Collections: d.Collections,
			Audit:       d.Audit,
			Notifier:    d.Notifier,
			Tx:          d.Tx,
			Now:         d.Now,
		}),
		Queries: query.New(query.Deps{Todos: d.Todos, Projects: d.Projects, Collections: d.Collections, Now: d.Now}),
	}
}

// RegisterRoutes mounts /todos, /projects and /collections; protect must
// authenticate the caller and resolve their team memberships
// (access.Middleware). limitCreate rate-limits creating todos; it runs after
// protect, so it can key on the caller.
func (m *Module) RegisterRoutes(mux *http.ServeMux, protect, limitCreate httpx.Middleware) {
	httpapi.Register(mux, m.Commands, m.Queries, protect, limitCreate)
}

// Todo is the read model exposed to other modules.
type Todo struct {
	ID        string
	UserID    string
	Title     string
	Status    string
	Completed bool
	CreatedAt time.Time
}

// ListForUser returns all of a user's personal todos (archived projects
// included), newest first.
func (m *Module) ListForUser(ctx context.Context, userID string) ([]Todo, error) {
	todos, err := m.Queries.ListTodos.Handle(ctx, query.ListTodos{Actor: access.Personal(userID), IncludeArchived: true})
	if err != nil {
		return nil, err
	}
	out := make([]Todo, 0, len(todos))
	for _, t := range todos {
		out = append(out, Todo{ID: t.ID, UserID: t.UserID, Title: t.Title, Status: string(t.Status), Completed: t.Completed(), CreatedAt: t.CreatedAt})
	}
	return out, nil
}

// DeleteAllForUser removes a user's personal todos, projects and
// collections; team records they created stay with the team. Called with a
// transactional context it joins that transaction.
func (m *Module) DeleteAllForUser(ctx context.Context, userID string) error {
	return m.Commands.DeleteUserData.Handle(ctx, command.DeleteUserData{UserID: userID})
}

// DeleteTeamData removes every todo, project and collection of a team.
// Called with a transactional context it joins that transaction.
func (m *Module) DeleteTeamData(ctx context.Context, teamID string) error {
	return m.Commands.DeleteTeamData.Handle(ctx, command.DeleteTeamData{TeamID: teamID})
}
