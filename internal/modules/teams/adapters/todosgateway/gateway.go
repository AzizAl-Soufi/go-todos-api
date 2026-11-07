// Package todosgateway connects the teams module to the todos module's
// public API: deleting a team deletes its projects, collections and todos.
package todosgateway

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos"
)

type Gateway struct{ todos *todos.Module }

var _ application.TeamData = Gateway{}

func New(m *todos.Module) Gateway { return Gateway{todos: m} }

func (g Gateway) DeleteTeamData(ctx context.Context, teamID string) error {
	return g.todos.DeleteTeamData(ctx, teamID)
}
