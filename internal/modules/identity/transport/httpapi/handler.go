// Package httpapi is the identity module's HTTP transport (driving adapter).
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
// (rate limiting); protect authenticates the caller.
func Register(mux *http.ServeMux, cmd command.Handlers, qry query.Handlers, protect, limit httpx.Middleware) {
	h := handler{cmd: cmd, qry: qry}
	public := map[string]httpx.Endpoint{
		"POST /auth/register":               h.register,
		"POST /auth/login":                  h.login,
		"POST /auth/refresh":                h.refresh,
		"POST /auth/logout":                 h.logout,
		"POST /auth/verify-email":           h.verifyEmail,
		"GET /auth/verify-email":            h.verifyEmailLink,
		"POST /auth/password/forgot":        h.forgotPassword,
		"POST /auth/password/reset":         h.resetPassword,
		"POST /auth/passkeys/login/options": h.beginPasskeyLogin,
		"POST /auth/passkeys/login":         h.finishPasskeyLogin,
	}
	private := map[string]httpx.Endpoint{
		"POST /auth/logout-all":          auth.ForUser(h.logoutAll),
		"POST /auth/verify-email/resend": auth.ForUser(h.resendVerification),
		"GET /me":                        auth.ForUser(h.me),
		"DELETE /me":                     auth.ForUser(h.deleteAccount),
		"POST /me/passkeys/options":      auth.ForUser(h.beginPasskeyRegistration),
		"POST /me/passkeys":              auth.ForUser(h.finishPasskeyRegistration),
		"GET /me/passkeys":               auth.ForUser(h.listPasskeys),
		"DELETE /me/passkeys/{id}":       auth.ForUser(h.deletePasskey),
	}
	for pattern, e := range public {
		mux.Handle(pattern, limit(e))
	}
	for pattern, e := range private {
		mux.Handle(pattern, protect(e))
	}
	// Rate-limited too: it checks a password, so it must not allow guessing.
	mux.Handle("POST /auth/password/change", limit(protect(auth.ForUser(h.changePassword))))
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

type tokensResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"` // seconds until the access token expires
}

func toTokens(t command.Tokens) tokensResponse {
	return tokensResponse{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, TokenType: "Bearer", ExpiresIn: int(t.ExpiresIn.Seconds())}
}

type authResponse struct {
	User          userResponse   `json:"user"`
	Authorization tokensResponse `json:"authorization"`
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
	registerRequest struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	loginRequest struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	refreshRequest struct {
		RefreshToken string `json:"refresh_token"`
	}
	tokenRequest struct {
		Token string `json:"token"`
	}
	forgotRequest struct {
		Email string `json:"email"`
	}
	resetRequest struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
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
	changePasswordRequest struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
)

// decodeRefresh reads {"refresh_token": "..."} and requires a non-empty token.
func decodeRefresh(r *http.Request) (string, error) {
	req, err := decode[refreshRequest](r)
	if err == nil && req.RefreshToken == "" {
		err = apperrors.Validation("INVALID_VALUE", "refresh_token is required")
	}
	return req.RefreshToken, err
}

// ---- auth ----

// signedIn answers registration and login: the issued credentials plus the
// account, read back through the query side.
func (h handler) signedIn(r *http.Request, status int, creds command.Credentials) (int, any, error) {
	user, err := h.qry.GetUser.Handle(r.Context(), query.GetUser{UserID: creds.UserID})
	if err != nil {
		return 0, nil, err
	}
	return status, authResponse{User: toUser(user), Authorization: toTokens(creds.Tokens)}, nil
}

func (h handler) register(r *http.Request) (int, any, error) {
	req, err := decode[registerRequest](r)
	if err != nil {
		return 0, nil, err
	}
	creds, err := h.cmd.Register.Handle(r.Context(), command.Register{Name: req.Name, Email: req.Email, Password: req.Password})
	if err != nil {
		return 0, nil, err
	}
	return h.signedIn(r, http.StatusCreated, creds)
}

func (h handler) login(r *http.Request) (int, any, error) {
	req, err := decode[loginRequest](r)
	if err != nil {
		return 0, nil, err
	}
	creds, err := h.cmd.Login.Handle(r.Context(), command.Login{Email: req.Email, Password: req.Password})
	if err != nil {
		return 0, nil, err
	}
	return h.signedIn(r, http.StatusOK, creds)
}

func (h handler) refresh(r *http.Request) (int, any, error) {
	token, err := decodeRefresh(r)
	if err != nil {
		return 0, nil, err
	}
	tokens, err := h.cmd.Refresh.Handle(r.Context(), command.Refresh{RefreshToken: token})
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, toTokens(tokens), nil
}

func (h handler) logout(r *http.Request) (int, any, error) {
	token, err := decodeRefresh(r)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.cmd.Logout.Handle(r.Context(), command.Logout{RefreshToken: token})
}

func (h handler) logoutAll(r *http.Request, userID string) (int, any, error) {
	return http.StatusNoContent, nil, h.cmd.LogoutAll.Handle(r.Context(), command.LogoutAll{UserID: userID})
}

// ---- email verification ----

func (h handler) verifyEmail(r *http.Request) (int, any, error) {
	req, err := decode[tokenRequest](r)
	if err != nil {
		return 0, nil, err
	}
	return h.doVerify(r, req.Token)
}

// verifyEmailLink serves the link sent by email: GET /auth/verify-email?token=...
func (h handler) verifyEmailLink(r *http.Request) (int, any, error) {
	return h.doVerify(r, r.URL.Query().Get("token"))
}

func (h handler) doVerify(r *http.Request, token string) (int, any, error) {
	if err := h.cmd.VerifyEmail.Handle(r.Context(), command.VerifyEmail{Token: token}); err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"emailVerified": true}, nil
}

func (h handler) resendVerification(r *http.Request, userID string) (int, any, error) {
	if err := h.cmd.ResendVerification.Handle(r.Context(), command.ResendVerification{UserID: userID}); err != nil {
		return 0, nil, err
	}
	return http.StatusAccepted, map[string]any{"sent": true}, nil
}

// ---- passwords ----

func (h handler) forgotPassword(r *http.Request) (int, any, error) {
	req, err := decode[forgotRequest](r)
	if err != nil {
		return 0, nil, err
	}
	if err := h.cmd.RequestPasswordReset.Handle(r.Context(), command.RequestPasswordReset{Email: req.Email}); err != nil {
		return 0, nil, err
	}
	// Same answer whether or not the account exists.
	return http.StatusAccepted, map[string]any{"accepted": true}, nil
}

func (h handler) resetPassword(r *http.Request) (int, any, error) {
	req, err := decode[resetRequest](r)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusNoContent, nil, h.cmd.ResetPassword.Handle(r.Context(), command.ResetPassword{Token: req.Token, Password: req.Password})
}

func (h handler) changePassword(r *http.Request, userID string) (int, any, error) {
	req, err := decode[changePasswordRequest](r)
	if err != nil {
		return 0, nil, err
	}
	tokens, err := h.cmd.ChangePassword.Handle(r.Context(), command.ChangePassword{
		UserID: userID, CurrentPassword: req.CurrentPassword, NewPassword: req.NewPassword,
	})
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"authorization": toTokens(tokens)}, nil
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
	creds, err := h.cmd.FinishPasskeyLogin.Handle(r.Context(), command.FinishPasskeyLogin{Token: req.Token, Response: req.Credential})
	if err != nil {
		return 0, nil, err
	}
	return h.signedIn(r, http.StatusOK, creds)
}
