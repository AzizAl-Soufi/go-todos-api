// Package auditgateway connects the identity module to the audit module: the
// personal security log.
package auditgateway

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
)

type Gateway struct{ audit *audit.Module }

var _ application.AuditLog = Gateway{}

func New(m *audit.Module) Gateway { return Gateway{audit: m} }

func (g Gateway) Record(ctx context.Context, e application.AuditEntry) error {
	return g.audit.Record(ctx, audit.Entry{UserID: e.UserID, ActorID: e.ActorID, Action: e.Action, Details: e.Details})
}

func (g Gateway) DeleteUserLog(ctx context.Context, userID string) error {
	return g.audit.DeleteUserLog(ctx, userID)
}
