// Package oauth is the oauth module's public surface: the OAuth 2.1
// authorization server that MCP hosts (Claude, Claude Code, other agents)
// use to act for a user on the MCP endpoint.
//
// Clients register with Dynamic Client Registration (RFC 7591) or identify
// themselves with a Client ID Metadata Document. The user signs in on this
// server's sign-in page with Firebase, approves the client, and the client
// exchanges the code (PKCE, S256) for an access token bound to the MCP
// endpoint (RFC 8707) and a rotating refresh token. Users see and revoke
// their connected apps under /me/connections; logging out everywhere and
// deleting the account revoke all of them.
//
// The module depends only on platform: it verifies Firebase ID tokens
// through auth.Authenticator, which the identity module implements.
//
// Layout (CQRS inside hexagonal layers):
//
//	domain/                clients, codes, grants, tokens, PKCE, redirect rules
//	application/           ports, authorization request checks
//	application/command/   register, approve, exchange, refresh, revoke
//	application/query/     check an authorization, authenticate tokens, list connections
//	transport/             driving adapter (HTTP: metadata, OAuth endpoints, sign-in page)
//	adapters/              driven adapters (persistence, client metadata documents)
package oauth

import (
	"context"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/transport/httpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

type (
	ClientStore = application.ClientRepository
	CodeStore   = application.CodeRepository
	GrantStore  = application.GrantRepository
	// ClientMetadataFetcher loads Client ID Metadata Documents.
	ClientMetadataFetcher = application.ClientMetadataFetcher
	// SignIn is the Firebase web configuration of the sign-in page.
	SignIn = httpapi.SignIn
	// Authenticated is a valid MCP access token's owner and limits.
	Authenticated = query.Authenticated
)

// ErrInvalidToken is an MCP access token that doesn't authenticate.
var ErrInvalidToken = query.ErrInvalidToken

type Config struct {
	// Issuer is the authorization server's URL (APP_BASE_URL).
	Issuer string
	// Resource is the MCP endpoint's URL, the audience of every token.
	Resource string
	SignIn   SignIn
}

type Deps struct {
	Clients  ClientStore
	Codes    CodeStore
	Grants   GrantStore
	Metadata application.ClientMetadataFetcher // nil: no metadata document clients
	// Accounts verifies the Firebase ID token posted by the sign-in page.
	Accounts auth.Authenticator
	Tx       tx.Runner
	Config   Config
	Now      func() time.Time // nil means time.Now
}

type Module struct {
	Commands command.Handlers
	Queries  query.Handlers
	cfg      Config
}

func New(d Deps) *Module {
	appCfg := application.Config{Issuer: d.Config.Issuer, Resource: d.Config.Resource}
	return &Module{
		Commands: command.New(command.Deps{
			Clients:  d.Clients,
			Codes:    d.Codes,
			Grants:   d.Grants,
			Metadata: d.Metadata,
			Accounts: d.Accounts,
			Tx:       d.Tx,
			Config:   appCfg,
			Now:      d.Now,
		}),
		Queries: query.New(query.Deps{
			Clients:  d.Clients,
			Grants:   d.Grants,
			Metadata: d.Metadata,
			Config:   appCfg,
			Now:      d.Now,
		}),
		cfg: d.Config,
	}
}

// RegisterRoutes mounts the discovery metadata, the OAuth endpoints and the
// sign-in page, and /me/connections. protect authenticates REST callers;
// limit rate-limits per client IP (the approval form); limitRegistration
// rate-limits client registration.
func (m *Module) RegisterRoutes(mux *http.ServeMux, protect, limit, limitRegistration httpx.Middleware) {
	httpapi.Register(mux, m.Commands, m.Queries, httpapi.Config{
		Issuer:   m.cfg.Issuer,
		Resource: m.cfg.Resource,
		SignIn:   m.cfg.SignIn,
	}, protect, limit, limitRegistration)
}

// AuthenticateToken resolves an MCP access token; ErrInvalidToken when it
// doesn't authenticate.
func (m *Module) AuthenticateToken(ctx context.Context, token string) (Authenticated, error) {
	return m.Queries.AuthenticateToken.Handle(ctx, query.AuthenticateToken{Token: token})
}

// RevokeUserConnections disconnects every app of a user (log out
// everywhere, account deletion).
func (m *Module) RevokeUserConnections(ctx context.Context, userID string) error {
	return m.Commands.RevokeUserConnections.Handle(ctx, command.RevokeUserConnections{UserID: userID})
}

// ProtectedResourceMetadataURL is where MCP clients find out how to get a
// token (RFC 9728), for the WWW-Authenticate header of a 401 from /mcp.
func (m *Module) ProtectedResourceMetadataURL() string {
	return httpapi.ProtectedResourceMetadataURL(m.cfg.Issuer)
}
