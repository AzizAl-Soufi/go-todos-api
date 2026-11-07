// Package identity is the identity module's public surface: accounts of
// Firebase Authentication users (Google, email and password), passkeys, and
// the Authenticator other modules rely on (via platform/auth), which accepts
// Firebase ID tokens.
//
// Layout (CQRS inside hexagonal layers):
//
//	domain/                entities and invariants
//	application/           ports
//	application/command/   write use cases, one handler per command
//	application/query/     read use cases, one handler per query
//	transport/             driving adapters (HTTP)
//	adapters/              driven adapters (persistence, Firebase, passkeys (WebAuthn), todos, teams and audit gateways)
package identity

import (
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/transport/httpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

type Config = command.Config

// DefaultConfig returns the production settings.
func DefaultConfig() Config { return command.DefaultConfig() }

// Deps are the adapters the module runs on.
type Deps struct {
	Users        application.UserRepository
	Passkeys     application.PasskeyRepository
	Challenges   application.PasskeyChallengeRepository
	RelyingParty application.RelyingParty
	IDTokens     application.IDTokenVerifier
	Firebase     application.FirebaseAdmin // nil without a service account
	Todos        application.TodoGateway
	Teams        application.TeamMemberships // nil: no teams to leave
	Audit        application.AuditLog        // nil records nothing
	Tx           tx.Runner
	Config       Config
	Now          func() time.Time // nil means time.Now
}

type Module struct {
	Commands command.Handlers
	Queries  query.Handlers
}

func New(d Deps) *Module {
	return &Module{
		Commands: command.New(command.Deps{
			Users: d.Users, Passkeys: d.Passkeys, Challenges: d.Challenges, RelyingParty: d.RelyingParty,
			IDTokens: d.IDTokens, Firebase: d.Firebase, Todos: d.Todos, Teams: d.Teams, Audit: d.Audit,
			Tx: d.Tx, Config: d.Config, Now: d.Now,
		}),
		Queries: query.New(query.Deps{Users: d.Users, Passkeys: d.Passkeys, IDTokens: d.IDTokens, Todos: d.Todos}),
	}
}

// Authenticator verifies Firebase ID tokens for platform/auth.Middleware.
func (m *Module) Authenticator() auth.Authenticator { return m.Queries.Authenticate }

// RegisterRoutes mounts /auth/*, /me and /me/passkeys. limit rate-limits the
// public auth endpoints; protect authenticates the caller.
func (m *Module) RegisterRoutes(mux *http.ServeMux, protect, limit httpx.Middleware) {
	httpapi.Register(mux, m.Commands, m.Queries, protect, limit)
}
