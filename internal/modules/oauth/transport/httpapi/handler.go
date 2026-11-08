// Package httpapi is the oauth module's HTTP transport: discovery metadata
// (RFC 9728, RFC 8414), dynamic client registration (RFC 7591), the
// authorization endpoint with its sign-in page, the token and revocation
// endpoints (RFC 6749, RFC 7009), and the user's connected apps.
//
// OAuth endpoints answer in OAuth's own error format
// ({"error": "...", "error_description": "..."}), which clients parse;
// /me/connections uses the API's usual errors.
package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

// Config is what the transport needs to describe this server.
type Config struct {
	Issuer   string // base URL
	Resource string // the MCP endpoint's URL
	SignIn   SignIn
}

const (
	authorizePath    = "/oauth/authorize"
	tokenPath        = "/oauth/token" //nolint:gosec // G101: a URL path, not a credential
	registrationPath = "/oauth/register"
	revocationPath   = "/oauth/revoke"
	resourceMetaPath = "/.well-known/oauth-protected-resource"
)

// ProtectedResourceMetadataURL is where the MCP endpoint's metadata is.
func ProtectedResourceMetadataURL(issuer string) string { return issuer + resourceMetaPath }

type handler struct {
	cmd  command.Handlers
	qry  query.Handlers
	cfg  Config
	page *signInPage
}

// Register mounts the routes. protect authenticates REST callers; limit
// rate-limits the approval form per client IP; limitRegistration rate-limits
// client registration. The token and revocation endpoints aren't rate
// limited: every Claude user's requests come from the same few addresses,
// and the codes and tokens they take can't be guessed.
func Register(mux *http.ServeMux, cmd command.Handlers, qry query.Handlers, cfg Config, protect, limit, limitRegistration httpx.Middleware) {
	h := handler{cmd: cmd, qry: qry, cfg: cfg, page: newSignInPage(cfg.SignIn)}

	// Read by MCP clients, browser-based ones included: open to any origin
	// (no cookies are involved).
	public := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, cors(fn))
	}
	public("GET "+resourceMetaPath, h.protectedResourceMetadata)
	public("GET "+resourceMetaPath+"/mcp", h.protectedResourceMetadata) // path-suffixed form (RFC 9728 section 3.1)
	public("GET /.well-known/oauth-authorization-server", h.authorizationServerMetadata)
	public("POST "+tokenPath, h.token)
	public("POST "+revocationPath, h.revoke)
	mux.Handle("POST "+registrationPath, cors(limitRegistration(http.HandlerFunc(h.register))))
	for _, p := range []string{resourceMetaPath, resourceMetaPath + "/mcp", "/.well-known/oauth-authorization-server", tokenPath, revocationPath, registrationPath} {
		mux.Handle("OPTIONS "+p, cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})))
	}

	mux.HandleFunc("GET "+authorizePath, h.authorizePage)
	mux.Handle("POST "+authorizePath, limit(http.HandlerFunc(h.authorizeDecision)))

	mux.Handle("GET /me/connections", protect(auth.ForUser(h.listConnections)))
	mux.Handle("DELETE /me/connections/{id}", protect(auth.ForUser(h.revokeConnection)))
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr := w.Header()
		hdr.Set("Access-Control-Allow-Origin", "*")
		hdr.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		hdr.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, MCP-Protocol-Version")
		hdr.Set("Access-Control-Max-Age", "86400")
		next.ServeHTTP(w, r)
	})
}

// ---- discovery ----

func (h handler) protectedResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	httpx.RespondJSON(w, http.StatusOK, map[string]any{
		"resource":                 h.cfg.Resource,
		"authorization_servers":    []string{h.cfg.Issuer},
		"scopes_supported":         []string{domain.Scope},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Todos API",
	})
}

func (h handler) authorizationServerMetadata(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	httpx.RespondJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         h.cfg.Issuer,
		"authorization_endpoint":                         h.cfg.Issuer + authorizePath,
		"token_endpoint":                                 h.cfg.Issuer + tokenPath,
		"registration_endpoint":                          h.cfg.Issuer + registrationPath,
		"revocation_endpoint":                            h.cfg.Issuer + revocationPath,
		"scopes_supported":                               []string{domain.Scope, domain.OfflineAccess},
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"code_challenge_methods_supported":               []string{"S256"},
		"client_id_metadata_document_supported":          true,
		"authorization_response_iss_parameter_supported": true,
	})
}

// ---- OAuth errors ----

// oauthError writes err in OAuth's format. Errors that aren't OAuth errors
// are logged and reported as server_error, so internals don't leak.
func oauthError(w http.ResponseWriter, r *http.Request, err error) {
	var oe *domain.Error
	if !errors.As(err, &oe) {
		slog.Error("oauth request failed", "path", r.URL.Path, "request_id", httpx.RequestIDFrom(r.Context()), "error", err)
		oe = &domain.Error{Code: "server_error", Description: "internal server error"}
	}
	status := http.StatusBadRequest
	switch oe.Code {
	case "invalid_client":
		status = http.StatusUnauthorized
	case "server_error":
		status = http.StatusInternalServerError
	}
	noStore(w)
	httpx.RespondJSON(w, status, map[string]string{"error": oe.Code, "error_description": oe.Description})
}

func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

// ---- registration ----

type registrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

func (h handler) register(w http.ResponseWriter, r *http.Request) {
	var req registrationRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		oauthError(w, r, domain.ErrInvalidClientMetadata)
		return
	}
	id, err := h.cmd.RegisterClient.Handle(r.Context(), command.RegisterClient{
		Name:                    req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
		GrantTypes:              req.GrantTypes,
		ResponseTypes:           req.ResponseTypes,
	})
	if err != nil {
		oauthError(w, r, err)
		return
	}
	client, err := h.qry.GetClient.Handle(r.Context(), query.GetClient{ID: id})
	if err != nil {
		oauthError(w, r, err)
		return
	}
	noStore(w)
	httpx.RespondJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  client.ID,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                client.Name,
		"redirect_uris":              client.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

// ---- authorization ----

func authorizationRequest(v url.Values) domain.AuthorizationRequest {
	return domain.AuthorizationRequest{
		ResponseType:        v.Get("response_type"),
		ClientID:            v.Get("client_id"),
		RedirectURI:         v.Get("redirect_uri"),
		State:               v.Get("state"),
		CodeChallenge:       v.Get("code_challenge"),
		CodeChallengeMethod: v.Get("code_challenge_method"),
		Resource:            v.Get("resource"),
		Scope:               v.Get("scope"),
	}
}

// authorizePage shows the sign-in and consent page for a valid request.
func (h handler) authorizePage(w http.ResponseWriter, r *http.Request) {
	auth, err := h.qry.CheckAuthorization.Handle(r.Context(), query.CheckAuthorization{Request: authorizationRequest(r.URL.Query())})
	if err != nil {
		h.authorizationFailed(w, r, err)
		return
	}
	h.page.render(w, r, auth.Client, auth.Request)
}

// authorizeDecision handles the sign-in page's form: approve (with the
// user's Firebase ID token) or deny.
func (h handler) authorizeDecision(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.page.error(w, r, http.StatusBadRequest, "The request is malformed.")
		return
	}
	req := authorizationRequest(r.PostForm)
	if r.PostForm.Get("decision") != "approve" {
		auth, err := h.qry.CheckAuthorization.Handle(r.Context(), query.CheckAuthorization{Request: req})
		if err != nil {
			h.authorizationFailed(w, r, err)
			return
		}
		h.redirect(w, r, auth.Request.RedirectURI, url.Values{
			"error": {domain.ErrAccessDenied.Code}, "state": {auth.Request.State},
		})
		return
	}
	code, err := h.cmd.Approve.Handle(r.Context(), command.Approve{Request: req, IDToken: r.PostForm.Get("id_token")})
	if err != nil {
		h.authorizationFailed(w, r, err)
		return
	}
	h.redirect(w, r, req.RedirectURI, url.Values{"code": {code}, "state": {req.State}})
}

// authorizationFailed reports a failed authorization: at the client's
// redirect URI when the client and redirect URI were valid, otherwise on
// an error page (redirecting to an unchecked URI would be an open
// redirect).
func (h handler) authorizationFailed(w http.ResponseWriter, r *http.Request, err error) {
	var redirectErr *application.RedirectError
	if errors.As(err, &redirectErr) {
		h.redirect(w, r, redirectErr.RedirectURI, url.Values{
			"error": {redirectErr.Err.Code}, "error_description": {redirectErr.Err.Description}, "state": {redirectErr.State},
		})
		return
	}
	var oe *domain.Error
	switch {
	case errors.As(err, &oe):
		h.page.error(w, r, http.StatusBadRequest, "This app's sign-in request is invalid ("+oe.Description+").")
	case isAppError(err, http.StatusUnauthorized), isAppError(err, http.StatusForbidden):
		h.page.error(w, r, http.StatusUnauthorized, "Your sign-in could not be verified. Sign in again and retry.")
	default:
		slog.Error("authorization failed", "request_id", httpx.RequestIDFrom(r.Context()), "error", err)
		h.page.error(w, r, http.StatusInternalServerError, "Something went wrong. Try again later.")
	}
}

func isAppError(err error, status int) bool {
	ae, ok := apperrors.From(err)
	return ok && ae.Status() == status
}

// redirect sends the browser back to the client with params, plus iss
// (RFC 9207) so the client can tell which server answered.
func (h handler) redirect(w http.ResponseWriter, r *http.Request, redirectURI string, params url.Values) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		h.page.error(w, r, http.StatusBadRequest, "The app's redirect address is invalid.")
		return
	}
	q := u.Query()
	for k, vs := range params {
		if len(vs) > 0 && vs[0] != "" {
			q.Set(k, vs[0])
		}
	}
	q.Set("iss", h.cfg.Issuer)
	u.RawQuery = q.Encode()
	noStore(w)
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

// ---- token endpoint ----

func (h handler) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, r, domain.ErrInvalidRequest)
		return
	}
	f := r.PostForm
	var (
		tokens command.TokenSet
		err    error
	)
	switch f.Get("grant_type") {
	case "authorization_code":
		tokens, err = h.cmd.ExchangeCode.Handle(r.Context(), command.ExchangeCode{
			ClientID:     f.Get("client_id"),
			Code:         f.Get("code"),
			RedirectURI:  f.Get("redirect_uri"),
			CodeVerifier: f.Get("code_verifier"),
			Resource:     f.Get("resource"),
		})
	case "refresh_token":
		tokens, err = h.cmd.Refresh.Handle(r.Context(), command.Refresh{
			ClientID:     f.Get("client_id"),
			RefreshToken: f.Get("refresh_token"),
			Resource:     f.Get("resource"),
			Scope:        f.Get("scope"),
		})
	case "":
		err = domain.ErrInvalidRequest
	default:
		err = domain.ErrUnsupportedGrantType
	}
	if err != nil {
		oauthError(w, r, err)
		return
	}
	noStore(w)
	httpx.RespondJSON(w, http.StatusOK, map[string]any{
		"access_token":  tokens.AccessToken,
		"token_type":    "Bearer",
		"expires_in":    int(tokens.ExpiresIn / time.Second),
		"refresh_token": tokens.RefreshToken,
		"scope":         tokens.Scope,
	})
}

func (h handler) revoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.PostForm.Get("token") == "" {
		oauthError(w, r, domain.ErrInvalidRequest)
		return
	}
	err := h.cmd.RevokeToken.Handle(r.Context(), command.RevokeToken{
		ClientID: r.PostForm.Get("client_id"),
		Token:    r.PostForm.Get("token"),
	})
	if err != nil {
		oauthError(w, r, err)
		return
	}
	noStore(w)
	w.WriteHeader(http.StatusOK)
}

// ---- connections ----

type connectionResponse struct {
	ID         string    `json:"id"`
	ClientID   string    `json:"clientId"`
	ClientName string    `json:"clientName"`
	Scope      string    `json:"scope"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}

func (h handler) listConnections(r *http.Request, userID string) (int, any, error) {
	grants, err := h.qry.ListConnections.Handle(r.Context(), query.ListConnections{UserID: userID})
	if err != nil {
		return 0, nil, err
	}
	out := make([]connectionResponse, 0, len(grants))
	for _, g := range grants {
		out = append(out, connectionResponse{
			ID:         g.ID,
			ClientID:   g.ClientID,
			ClientName: g.ClientName,
			Scope:      g.Scope,
			CreatedAt:  g.CreatedAt,
			LastUsedAt: g.LastUsedAt,
		})
	}
	return http.StatusOK, out, nil
}

func (h handler) revokeConnection(r *http.Request, userID string) (int, any, error) {
	err := h.cmd.RevokeConnection.Handle(r.Context(), command.RevokeConnection{UserID: userID, ID: r.PathValue("id")})
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, nil
}
