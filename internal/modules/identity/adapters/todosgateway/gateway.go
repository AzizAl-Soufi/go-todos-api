// Package todosgateway connects the identity module to the todos module's
// public API. It is the only place in identity that imports the todos module.
package todosgateway

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos"
)

type Gateway struct {
	todos *todos.Module
}

var _ application.TodoGateway = Gateway{}

func New(m *todos.Module) Gateway {
	return Gateway{todos: m}
}

func (g Gateway) ListForUser(ctx context.Context, userID string) ([]application.TodoSummary, error) {
	list, err := g.todos.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]application.TodoSummary, 0, len(list))
	for _, t := range list {
		out = append(out, application.TodoSummary{ID: t.ID, UserID: t.UserID, Title: t.Title, Status: t.Status, Completed: t.Completed, CreatedAt: t.CreatedAt})
	}
	return out, nil
}

func (g Gateway) DeleteAllForUser(ctx context.Context, userID string) error {
	return g.todos.DeleteAllForUser(ctx, userID)
}
