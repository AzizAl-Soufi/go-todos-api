// Package notificationsgateway connects the todos module to the
// notifications module: new team todos may notify the team.
package notificationsgateway

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
)

type Gateway struct{ notifications *notifications.Module }

var _ application.Notifier = Gateway{}

func New(m *notifications.Module) Gateway { return Gateway{notifications: m} }

func (g Gateway) TodoCreated(ctx context.Context, t application.NewTeamTodo) {
	g.notifications.TodoCreated(ctx, notifications.TodoCreated{
		TeamID:    t.TeamID,
		ProjectID: t.ProjectID,
		TodoID:    t.TodoID,
		Title:     t.Title,
		ActorID:   t.ActorID,
		ActorName: t.ActorName,
		ActorRole: t.ActorRole,
	})
}
