package main

import (
	"context"
	"errors"
	"net/http"
	"runtime/debug"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

// mcpPath is the MCP endpoint, relative to APP_BASE_URL.
const mcpPath = "/mcp"

const mcpInstructions = "Todos, projects, collections and teams of the signed-in user, " +
	"as in the Todos REST API. Records are personal or belong to a team; team roles " +
	"(owner, admin, member, viewer) decide what may be changed. IDs come from the list tools. " +
	"Times are RFC 3339 in UTC."

// newMCPHandler serves the MCP endpoint: Streamable HTTP, stateless (every
// request stands alone, so any instance can serve it and nothing is kept
// between requests), JSON responses. Requests need an OAuth access token
// from this server's authorization server; the token's account becomes the
// caller exactly as for the REST API (resolve adds team roles), and the
// tools call the same handlers as the REST endpoints.
func newMCPHandler(cfg *config.Config, m modules, resolve httpx.Middleware, limitTodoCreates func(string) error) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "todos-api", Title: "Todos", Version: buildVersion()},
		&mcp.ServerOptions{Instructions: mcpInstructions})
	m.identity.RegisterTools(server)
	m.todos.RegisterTools(server, limitTodoCreates)
	m.teams.RegisterTools(server)
	m.audit.RegisterTools(server)

	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// The localhost check guards unauthenticated local servers against DNS
		// rebinding. Every request here needs a bearer token a rebinding page
		// can't have, and the check would reject a reverse proxy on the same
		// host forwarding Host: api.example.com.
		DisableLocalhostProtection: true,
	})

	verify := func(ctx context.Context, token string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		a, err := m.oauth.AuthenticateToken(ctx, token)
		if errors.Is(err, oauth.ErrInvalidToken) {
			return nil, sdkauth.ErrInvalidToken
		}
		if err != nil {
			return nil, err
		}
		return &sdkauth.TokenInfo{
			Scopes:     a.Scopes,
			Expiration: a.ExpiresAt,
			UserID:     a.Principal.UserID,
			Extra:      map[string]any{"principal": a.Principal},
		}, nil
	}
	// A request without a valid token gets 401 with WWW-Authenticate pointing
	// at the protected resource metadata: that starts the client's sign-in.
	bearer := sdkauth.RequireBearerToken(verify, &sdkauth.RequireBearerTokenOptions{
		ResourceMetadataURL: m.oauth.ProtectedResourceMetadataURL(),
		Scopes:              []string{"todos"},
	})

	var h http.Handler = transport
	h = resolve(h)
	if cfg.Auth.RequireVerifiedEmail {
		h = auth.RequireVerifiedEmail(h)
	}
	h = asPrincipal(h)
	h = bearer(h)
	return mcpCORS(h)
}

// asPrincipal turns the verified token into the auth.Principal the rest of
// the API works with.
func asPrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p auth.Principal
		ok := false
		if info := sdkauth.TokenInfoFromContext(r.Context()); info != nil {
			p, ok = info.Extra["principal"].(auth.Principal)
		}
		if !ok {
			httpx.RespondErr(w, r, auth.ErrNoPrincipal)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
	})
}

// mcpCORS lets browser-based MCP clients call the endpoint. Credentials are
// bearer tokens in a header, never cookies, so any origin is safe.
func mcpCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Session-Id, MCP-Protocol-Version, Last-Event-ID")
		h.Set("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id")
		h.Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
