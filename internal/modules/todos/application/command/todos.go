package command

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
)

// CreateTodo adds a todo, in a project or in the inbox.
type CreateTodo struct {
	UserID  string
	Changes domain.Changes // the fields to set; Title is required
}

type CreateTodoHandler struct{ base }

func (h CreateTodoHandler) Handle(ctx context.Context, cmd CreateTodo) (id string, err error) {
	if projectID, ok := cmd.Changes.ProjectID.Get(); ok {
		if err := h.requireProject(ctx, cmd.UserID, projectID); err != nil {
			return "", err
		}
	}
	todo, err := domain.New(cmd.UserID, cmd.Changes, h.now())
	if err != nil {
		return "", err
	}
	if err := h.Todos.Create(ctx, todo); err != nil {
		return "", err
	}
	return todo.ID, nil
}

// UpdateTodo changes the fields set in Changes.
type UpdateTodo struct {
	UserID, ID string
	Changes    domain.Changes
}

type UpdateTodoHandler struct{ base }

func (h UpdateTodoHandler) Handle(ctx context.Context, cmd UpdateTodo) error {
	todo, err := h.Todos.Get(ctx, cmd.UserID, cmd.ID)
	if err != nil || cmd.Changes.Empty() {
		return err
	}
	if projectID, ok := cmd.Changes.ProjectID.Get(); ok {
		if err := h.requireProject(ctx, cmd.UserID, projectID); err != nil {
			return err
		}
	}
	if err := todo.Apply(cmd.Changes, h.now()); err != nil {
		return err
	}
	return h.Todos.Update(ctx, todo)
}

type DeleteTodo struct {
	UserID, ID string
}

type DeleteTodoHandler struct{ base }

func (h DeleteTodoHandler) Handle(ctx context.Context, cmd DeleteTodo) error {
	return h.Todos.Delete(ctx, cmd.UserID, cmd.ID)
}
