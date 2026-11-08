// Package audit is the audit module's public surface: an append-only log of
// team activity (membership, roles, invitations, team content) and of each
// user's account security (logins, password changes, session revocations).
//
// Other modules record events through their own audit port, implemented by an
// auditgateway adapter that calls Record. Events are written with the
// caller's context, so inside a transaction they commit or roll back together
// with the change they describe.
//
// Layout (CQRS inside hexagonal layers):
//
//	domain/                the event, cursors and the page filter
//	application/           ports
//	application/command/   record and delete events
//	application/query/     read a team log or a security log
//	transport/             driving adapters (HTTP, MCP)
//	adapters/              driven adapters (persistence)
package audit

import (
	"context"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/transport/httpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/transport/mcpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

type EventStore = application.EventStore

type Deps struct {
	Events EventStore
	Now    func() time.Time // nil means time.Now
}

type Module struct {
	Commands command.Handlers
	Queries  query.Handlers
}

func New(d Deps) *Module {
	return &Module{
		Commands: command.New(command.Deps{Events: d.Events, Now: d.Now}),
		Queries:  query.New(query.Deps{Events: d.Events}),
	}
}

// RegisterRoutes mounts GET /teams/{id}/audit-log and GET /me/audit-log;
// protect must authenticate the caller and resolve their team memberships
// (access.Middleware).
func (m *Module) RegisterRoutes(mux *http.ServeMux, protect httpx.Middleware) {
	httpapi.Register(mux, m.Queries, protect)
}

// RegisterTools adds the audit log MCP tools to s. They run as the
// access.Actor that the /mcp endpoint's middleware puts in the context.
func (m *Module) RegisterTools(s *mcp.Server) {
	mcpapi.Register(s, m.Queries)
}

// Entry is an event as other modules describe it. Exactly one of TeamID and
// UserID is set.
type Entry struct {
	TeamID     string
	UserID     string
	ActorID    string
	Action     string
	TargetType string
	TargetID   string
	Details    map[string]string
}

// Record appends an entry, adding the client's IP and User-Agent from ctx.
func (m *Module) Record(ctx context.Context, e Entry) error {
	client := httpx.ClientFrom(ctx)
	return m.Commands.Record.Handle(ctx, command.Record{
		TeamID: e.TeamID, UserID: e.UserID, ActorID: e.ActorID, Action: e.Action,
		TargetType: e.TargetType, TargetID: e.TargetID, Details: e.Details,
		IP: client.IP, UserAgent: client.UserAgent,
	})
}

// DeleteUserLog removes a user's security log (account deletion).
func (m *Module) DeleteUserLog(ctx context.Context, userID string) error {
	return m.Commands.DeleteUserLog.Handle(ctx, command.DeleteUserLog{UserID: userID})
}

// DeleteTeamLog removes a team's audit log (team deletion).
func (m *Module) DeleteTeamLog(ctx context.Context, teamID string) error {
	return m.Commands.DeleteTeamLog.Handle(ctx, command.DeleteTeamLog{TeamID: teamID})
}
