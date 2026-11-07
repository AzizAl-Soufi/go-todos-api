// Package httpapi is the identity module's HTTP transport (driving adapter).
// Callers authenticate with Firebase ID tokens (Authorization: Bearer).
// Each endpoint decodes the request, runs one command and, where the response
// needs data, reads it back with a query.
package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

type handler struct {
	cmd command.Handlers
	qry query.Handlers
}

// Register mounts the routes. limit guards the unauthenticated endpoints
// (rate limiting); protect authenticates the caller by Firebase ID token.
func Register(mux *http.ServeMux, cmd command.Handlers, qry query.Handlers, protect, limit httpx.Middleware) {
	h := handler{cmd: cmd, qry: qry}
	public := map[string]httpx.Endpoint{
		"POST /auth":                        h.signIn,
		"POST /auth/passkeys/login/options": h.beginPasskeyLogin,
		"POST /auth/passkeys/login":         h.finishPasskeyLogin,
	}
	private := map[string]httpx.Endpoint{
		"POST /auth/logout-all":     auth.ForUser(h.logoutAll),
		"GET /me":                   auth.ForUser(h.me),
		"DELETE /me":                auth.ForUser(h.deleteAccount),
		"POST /me/passkeys/options": auth.ForUser(h.beginPasskeyRegistration),
		"POST /me/passkeys":         auth.ForUser(h.finishPasskeyRegistration),
		"GET /me/passkeys":          auth.ForUser(h.listPasskeys),
		"DELETE /me/passkeys/{id}":  auth.ForUser(h.deletePasskey),
	}
	for pattern, e := range public {
		mux.Handle(pattern, limit(e))
	}
	for pattern, e := range private {
		mux.Handle(pattern, protect(e))
	}
}

// ---- response shapes ----

type userResponse struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"emailVerified"`
	CreatedAt     time.Time `json:"createdAt"`
}

func toUser(u *domain.User) userResponse {
	return userResponse{ID: u.ID, Name: u.Name, Email: u.Email, EmailVerified: u.EmailVerified(), CreatedAt: u.CreatedAt}
}

type signInResponse struct {
	User    userResponse `json:"user"`
	Created bool         `json:"created"`
}

type passkeySignInResponse struct {
	User userResponse `json:"user"`
	// FirebaseCustomToken is exchanged with Firebase signInWithCustomToken
	// for an ID token.
	FirebaseCustomToken string `json:"firebase_custom_token"`
}

type todoResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Completed bool      `json:"completed"`
	CreatedAt time.Time `json:"createdAt"`
}

type overviewResponse struct {
	userResponse
	Todos []todoResponse `json:"todos"`
}

// ---- requests ----

func decode[T any](r *http.Request) (T, error) {
	var req T
	err := httpx.DecodeJSON(r, &req)
	return req, err
}

// Request bodies are decoded into these transport-owned types and copied into
// commands explicitly, so a client can never set a command field (such as a
// user ID) that the API doesn't expose.
type (
	// credential is the PublicKeyCredential the browser returned, serialized
	// with toJSON(); it is handed to the WebAuthn adapter as is.
	passkeyRegistrationRequest struct {
		Token      string          `json:"token"`
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	passkeyLoginRequest struct {
		Token      string          `json:"token"`
		Credential json.RawMessage `json:"credential"`
	}
)

// ---- auth ----

func (h handler) user(r *http.Request, id string) (userResponse, error) {
	user, err := h.qry.GetUser.Handle(r.Context(), query.GetUser{UserID: id})
	if err != nil {
		return userResponse{}, err
	}
	return toUser(user), nil
}

// signIn is called after every Firebase sign-in, with the Firebase ID token
// as "Authorization: Bearer"; it creates or links the account on the first
// one. It reads the header itself rather than running behind the auth
// middleware, which only admits users that already have an account.
func (h handler) signIn(r *http.Request) (int, any, error) {
	idToken, err := auth.BearerToken(r)
	if err != nil {
		return 0, nil, err
	}
	res, err := h.cmd.SignIn.Handle(r.Context(), command.SignIn{IDToken: idToken})
	if err != nil {
		return 0, nil, err
	}
	user, err := h.user(r, res.UserID)
	if err != nil {
		return 0, nil, err
	}
	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	return status, signInResponse{User: user, Created: res.Created}, nil
}

func (h handler) logoutAll(r *http.Request, userID string) (int, any, error) {
	return http.StatusNoContent, nil, h.cmd.LogoutAll.Handle(r.Context(), command.LogoutAll{UserID: userID})
}

// ---- account ----

func (h handler) me(r *http.Request, userID string) (int, any, error) {
	ov, err := h.qry.GetOverview.Handle(r.Context(), query.GetOverview{UserID: userID})
	if err != nil {
		return 0, nil, err
	}
	todos := make([]todoResponse, 0, len(ov.Todos))
	for _, t := range ov.Todos {
		todos = append(todos, todoResponse{ID: t.ID, UserID: t.UserID, Title: t.Title, Status: t.Status, Completed: t.Completed, CreatedAt: t.CreatedAt})
	}
	return http.StatusOK, overviewResponse{userResponse: toUser(ov.User), Todos: todos}, nil
}

func (h handler) deleteAccount(r *http.Request, userID string) (int, any, error) {
	return http.StatusNoContent, nil, h.cmd.DeleteAccount.Handle(r.Context(), command.DeleteAccount{UserID: userID})
}

// ---- passkeys ----

type passkeyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

func toPasskey(p *domain.Passkey) passkeyResponse {
	return passkeyResponse{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt}
}

// passkeyOptionsResponse carries the options for navigator.credentials
// ({"publicKey": {...}}) and the token to send back with the answer.
type passkeyOptionsResponse struct {
	Token   string          `json:"token"`
	Options json.RawMessage `json:"options"`
}

func toPasskeyOptions(o command.PasskeyOptions) passkeyOptionsResponse {
	return passkeyOptionsResponse{Token: o.Token, Options: o.Options}
}

func requireCredential(c json.RawMessage) error {
	if len(c) == 0 || string(c) == "null" {
		return apperrors.Validation("INVALID_VALUE", "credential is required")
	}
	return nil
}

func (h handler) beginPasskeyRegistration(r *http.Request, userID string) (int, any, error) {
	opts, err := h.cmd.BeginPasskeyRegistration.Handle(r.Context(), command.BeginPasskeyRegistration{UserID: userID})
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, toPasskeyOptions(opts), nil
}

func (h handler) finishPasskeyRegistration(r *http.Request, userID string) (int, any, error) {
	req, err := decode[passkeyRegistrationRequest](r)
	if err == nil {
		err = requireCredential(req.Credential)
	}
	if err != nil {
		return 0, nil, err
	}
	id, err := h.cmd.FinishPasskeyRegistration.Handle(r.Context(), command.FinishPasskeyRegistration{
		UserID: userID, Token: req.Token, Name: req.Name, Response: req.Credential,
	})
	if err != nil {
		return 0, nil, err
	}
	passkeys, err := h.qry.ListPasskeys.Handle(r.Context(), query.ListPasskeys{UserID: userID})
	if err != nil {
		return 0, nil, err
	}
	for _, p := range passkeys {
		if p.ID == id {
			return http.StatusCreated, toPasskey(p), nil
		}
	}
	return 0, nil, domain.ErrPasskeyNotFound // deleted concurrently
}

func (h handler) listPasskeys(r *http.Request, userID string) (int, any, error) {
	passkeys, err := h.qry.ListPasskeys.Handle(r.Context(), query.ListPasskeys{UserID: userID})
	if err != nil {
		return 0, nil, err
	}
	out := make([]passkeyResponse, 0, len(passkeys))
	for _, p := range passkeys {
		out = append(out, toPasskey(p))
	}
	return http.StatusOK, out, nil
}

func (h handler) deletePasskey(r *http.Request, userID string) (int, any, error) {
	err := h.cmd.DeletePasskey.Handle(r.Context(), command.DeletePasskey{UserID: userID, PasskeyID: r.PathValue("id")})
	return http.StatusNoContent, nil, err
}

func (h handler) beginPasskeyLogin(r *http.Request) (int, any, error) {
	opts, err := h.cmd.BeginPasskeyLogin.Handle(r.Context(), command.BeginPasskeyLogin{})
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, toPasskeyOptions(opts), nil
}

func (h handler) finishPasskeyLogin(r *http.Request) (int, any, error) {
	req, err := decode[passkeyLoginRequest](r)
	if err == nil {
		err = requireCredential(req.Credential)
	}
	if err != nil {
		return 0, nil, err
	}
	res, err := h.cmd.FinishPasskeyLogin.Handle(r.Context(), command.FinishPasskeyLogin{Token: req.Token, Response: req.Credential})
	if err != nil {
		return 0, nil, err
	}
	user, err := h.user(r, res.UserID)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, passkeySignInResponse{User: user, FirebaseCustomToken: res.CustomToken}, nil
}
