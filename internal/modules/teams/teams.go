// Package teams is the teams module's public surface: teams with roles
// (owner, admin, member, viewer), email invitations and ownership transfer.
// Teams own projects, collections and todos in the todos module; this module
// tells everyone else who belongs where through access.Resolver.
//
// Layout (CQRS inside hexagonal layers):
//
//	domain/                teams, members, invitations, role rules
//	application/           ports
//	application/command/   write use cases, one handler per command
//	application/query/     read use cases, one handler per query
//	transport/             driving adapter (HTTP)
//	adapters/              driven adapters (persistence, mail, todos and audit gateways)
package teams

import (
	"context"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/transport/httpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

type (
	Config = command.Config
	// Stores are the persistence adapters; each serves commands and queries.
	TeamStore       = application.TeamRepository
	MemberStore     = application.MemberRepository
	InvitationStore = application.InvitationRepository
)

// DefaultConfig returns a 7-day invitation lifetime and links under baseURL.
func DefaultConfig(baseURL string) Config { return command.DefaultConfig(baseURL) }

type Deps struct {
	Teams       TeamStore
	Members     MemberStore
	Invitations InvitationStore
	TeamData    application.TeamData
	Audit       application.AuditLog
	Mailer      application.Mailer
	Tx          tx.Runner
	Config      Config
	Now         func() time.Time // nil means time.Now
}

type Module struct {
	Commands command.Handlers
	Queries  query.Handlers
}

func New(d Deps) *Module {
	return &Module{
		Commands: command.New(command.Deps{
			Teams: d.Teams, Members: d.Members, Invitations: d.Invitations, TeamData: d.TeamData,
			Audit: d.Audit, Mailer: d.Mailer, Tx: d.Tx, Config: d.Config, Now: d.Now,
		}),
		Queries: query.New(query.Deps{Teams: d.Teams, Members: d.Members, Invitations: d.Invitations}),
	}
}

// Resolver loads a caller's memberships for access.Middleware.
func (m *Module) Resolver() access.Resolver { return m.Queries.Memberships }

// NewDirectory looks up team rosters (access.Directory) straight from the
// stores, without the rest of the module. Modules that teams depends on
// (notifications, through todos) are built before it, so they get their
// directory from here instead of from a Module.
func NewDirectory(teams application.TeamReader, members application.MemberReader) access.Directory {
	return query.RosterHandler{Deps: query.Deps{Teams: teams, Members: members}}
}

// RegisterRoutes mounts /teams/* and /invitations/*. protect must
// authenticate the caller and resolve them into an access.Actor; limit
// rate-limits the token endpoints.
func (m *Module) RegisterRoutes(mux *http.ServeMux, protect, limit httpx.Middleware) {
	httpapi.Register(mux, m.Commands, m.Queries, protect, limit)
}

// RemoveUser takes a deleted account out of every team (see
// command.RemoveUser). Called with a transactional context it joins that
// transaction.
func (m *Module) RemoveUser(ctx context.Context, userID string) error {
	return m.Commands.RemoveUser.Handle(ctx, command.RemoveUser{UserID: userID})
}
