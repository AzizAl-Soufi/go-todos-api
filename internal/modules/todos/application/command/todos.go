package command

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
)

// CreateTodo adds a todo, in a project or in the (personal) inbox. A todo in
// a team project belongs to that team, and once it is committed the
// notifications module hears about it (it tells the team when an owner or
// admin added it).
type CreateTodo struct {
	Actor   access.Actor
	Changes domain.Changes // the fields to set; Title is required
}

type CreateTodoHandler struct{ base }

func (h CreateTodoHandler) Handle(ctx context.Context, cmd CreateTodo) (id string, err error) {
	teamID, err := h.teamOfProject(ctx, cmd.Actor, cmd.Changes.ProjectID)
	if err != nil {
		return "", err
	}
	todo, err := domain.New(cmd.Actor.UserID, cmd.Changes, h.now())
	if err != nil {
		return "", err
	}
	todo.TeamID = teamID
	err = h.commit(ctx,
		func(ctx context.Context) error { return h.Todos.Create(ctx, todo) },
		func() application.AuditEntry {
			return event(cmd.Actor, teamID, "todo.created", "todo", todo.ID, map[string]string{"title": todo.Title})
		})
	if err != nil {
		return "", err
	}
	if teamID != "" {
		role, _ := cmd.Actor.Role(teamID)
		projectID, _ := cmd.Changes.ProjectID.Get()
		h.Notifier.TodoCreated(ctx, application.NewTeamTodo{
			TeamID:    teamID,
			ProjectID: projectID,
			TodoID:    todo.ID,
			Title:     todo.Title,
			ActorID:   cmd.Actor.UserID,
			ActorName: cmd.Actor.Name,
			ActorRole: role,
		})
	}
	return todo.ID, nil
}

// teamOfProject resolves the team a todo gets in the project (inbox:
// personal) and checks the actor may add todos there.
func (b base) teamOfProject(ctx context.Context, a access.Actor, projectID domain.Patch[string]) (string, error) {
	id, ok := projectID.Get()
	if !ok {
		return "", nil
	}
	p, err := b.project(ctx, a, id)
	if err != nil {
		return "", err
	}
	return p.TeamID, authorize(a, p.TeamID, access.Role.CanEdit)
}

// UpdateTodo changes the fields set in Changes. Moving a todo to a project of
// another team (or to the inbox) moves it to that team (or makes it the
// actor's personal todo); the actor must be allowed to edit on both sides.
type UpdateTodo struct {
	Actor   access.Actor
	ID      string
	Changes domain.Changes
}

type UpdateTodoHandler struct{ base }

func (h UpdateTodoHandler) Handle(ctx context.Context, cmd UpdateTodo) error {
	scope := application.ScopeOf(cmd.Actor)
	todo, err := h.Todos.Get(ctx, scope, cmd.ID)
	if err != nil {
		return err
	}
	if err := authorize(cmd.Actor, todo.TeamID, access.Role.CanEdit); err != nil || cmd.Changes.Empty() {
		return err
	}
	from := todo.TeamID
	to := from
	if cmd.Changes.ProjectID.Set {
		if to, err = h.teamOfProject(ctx, cmd.Actor, cmd.Changes.ProjectID); err != nil {
			return err
		}
	}
	if err := todo.Apply(cmd.Changes, h.now()); err != nil {
		return err
	}
	todo.TeamID = to
	if to == "" && from != "" { // leaves the team: now the actor's own
		todo.UserID = cmd.Actor.UserID
	}

	details := map[string]string{"title": todo.Title, "fields": fields(cmd.Changes.Fields())}
	describe := func(teamID string) func() application.AuditEntry {
		return func() application.AuditEntry {
			return event(cmd.Actor, teamID, "todo.updated", "todo", todo.ID, details)
		}
	}
	events := []func() application.AuditEntry{describe(from)}
	if to != from {
		events = append(events, describe(to))
	}
	return h.commit(ctx, func(ctx context.Context) error { return h.Todos.Update(ctx, scope, todo) }, events...)
}

type DeleteTodo struct {
	Actor access.Actor
	ID    string
}

type DeleteTodoHandler struct{ base }

func (h DeleteTodoHandler) Handle(ctx context.Context, cmd DeleteTodo) error {
	scope := application.ScopeOf(cmd.Actor)
	todo, err := h.Todos.Get(ctx, scope, cmd.ID)
	if err != nil {
		return err
	}
	if err := authorize(cmd.Actor, todo.TeamID, access.Role.CanEdit); err != nil {
		return err
	}
	return h.commit(ctx,
		func(ctx context.Context) error { return h.Todos.Delete(ctx, scope, todo.ID) },
		func() application.AuditEntry {
			return event(cmd.Actor, todo.TeamID, "todo.deleted", "todo", todo.ID, map[string]string{"title": todo.Title})
		})
}
