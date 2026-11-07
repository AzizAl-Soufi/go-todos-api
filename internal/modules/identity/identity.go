// Package identity is the identity module's public surface: accounts,
// password login, password reset and change, refresh-token sessions, email
// verification and the Authenticator other modules rely on (via
// platform/auth).
//
// Layout (CQRS inside hexagonal layers):
//
//	domain/                entities and invariants
//	application/           ports
//	application/command/   write use cases, one handler per command
//	application/query/     read use cases, one handler per query
//	transport/             driving adapters (HTTP)
//	adapters/              driven adapters (persistence, security, mail, todos, teams and audit gateways)
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

// DefaultConfig returns production TTLs and email links under baseURL.
func DefaultConfig(baseURL string) Config { return command.DefaultConfig(baseURL) }

// Deps are the adapters the module runs on.
type Deps struct {
	Users          application.UserRepository
	Sessions       application.SessionRepository
	Verifications  application.OneTimeTokenRepository
	PasswordResets application.OneTimeTokenRepository
	Hasher         application.PasswordHasher
	Tokens         application.AccessTokens
	Mailer         application.Mailer
	Todos          application.TodoGateway
	Teams          application.TeamMemberships // nil: no teams to leave
	Audit          application.AuditLog        // nil records nothing
	Tx             tx.Runner
	Config         Config
	Now            func() time.Time // nil means time.Now
}

type Module struct {
	Commands command.Handlers
	Queries  query.Handlers
}

func New(d Deps) *Module {
	return &Module{
		Commands: command.New(command.Deps{
			Users: d.Users, Sessions: d.Sessions, Verifications: d.Verifications, PasswordResets: d.PasswordResets,
			Hasher: d.Hasher, Tokens: d.Tokens, Mailer: d.Mailer, Todos: d.Todos, Teams: d.Teams, Audit: d.Audit,
			Tx: d.Tx, Config: d.Config, Now: d.Now,
		}),
		Queries: query.New(query.Deps{Users: d.Users, Tokens: d.Tokens, Todos: d.Todos}),
	}
}

// Authenticator verifies access tokens for platform/auth.Middleware.
func (m *Module) Authenticator() auth.Authenticator { return m.Queries.Authenticate }

// RegisterRoutes mounts /auth/* and /me. limit rate-limits the public auth
// endpoints (and password change); protect authenticates the caller.
func (m *Module) RegisterRoutes(mux *http.ServeMux, protect, limit httpx.Middleware) {
	httpapi.Register(mux, m.Commands, m.Queries, protect, limit)
}
