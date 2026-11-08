// Package notificationsgateway connects the todos module to the
// notifications module: new team todos may notify the team, and due-date
// alerts go to the todo's user.
package notificationsgateway

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
)

type Gateway struct{ notifications *notifications.Module }

var _ application.Notifier = Gateway{}

func New(m *notifications.Module) Gateway { return Gateway{notifications: m} }

func (g Gateway) TodoCreated(ctx context.Context, t application.NewTeamTodo) error {
	return g.notifications.TodoCreated(ctx, notifications.TodoCreated{
		TeamID:    t.TeamID,
		ProjectID: t.ProjectID,
		TodoID:    t.TodoID,
		Title:     t.Title,
		ActorID:   t.ActorID,
		ActorName: t.ActorName,
		ActorRole: t.ActorRole,
	})
}

func (g Gateway) TodoDue(ctx context.Context, a application.TodoDueAlert, at time.Time) error {
	kind := notifications.DueSoon
	if a.Kind == domain.AlertOverdue {
		kind = notifications.Overdue
	}
	return g.notifications.TodoDue(ctx, notifications.DueAlert{
		UserID:    a.UserID,
		TodoID:    a.TodoID,
		Title:     a.Title,
		TeamID:    a.TeamID,
		ProjectID: a.ProjectID,
		Kind:      kind,
		DueAt:     a.DueAt,
	}, at)
}
