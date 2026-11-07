// Package auditgateway connects the todos module to the audit module: changes
// to team records go to the team audit log.
package auditgateway

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
)

type Gateway struct{ audit *audit.Module }

var _ application.AuditLog = Gateway{}

func New(m *audit.Module) Gateway { return Gateway{audit: m} }

func (g Gateway) Record(ctx context.Context, e application.AuditEntry) error {
	return g.audit.Record(ctx, audit.Entry{
		TeamID: e.TeamID, ActorID: e.ActorID, Action: e.Action, TargetType: e.TargetType, TargetID: e.TargetID, Details: e.Details,
	})
}
