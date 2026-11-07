package main

// End-to-end tests through the production router and module wiring, on the
// in-memory backend. They exercise the HTTP contract of every feature.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/security"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/testutil"
	"golang.org/x/crypto/bcrypt"
)

// captureMailer records email links instead of sending them.
type captureMailer struct {
	mu      sync.Mutex
	links   map[string]string // email -> latest verification link
	resets  map[string]string // email -> latest password reset link
	invites map[string]string // email -> latest team invitation link
}

func (m *captureMailer) SendInvitation(_ context.Context, to, _, _ string, _ access.Role, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invites[to] = link
	return nil
}

// inviteToken returns the token of the latest invitation sent to email.
func (m *captureMailer) inviteToken(t *testing.T, email string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	return linkToken(t, m.invites[email])
}

func (m *captureMailer) SendVerificationEmail(_ context.Context, to, _, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links[to] = link
	return nil
}

func (m *captureMailer) SendPasswordResetEmail(_ context.Context, to, _, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resets[to] = link
	return nil
}

func (m *captureMailer) token(t *testing.T, email string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	return linkToken(t, m.links[email])
}

// resetLink waits for and takes the password reset link sent to email: it is
// mailed in the background after the request returns.
func (m *captureMailer) resetLink(t *testing.T, email string) string {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		m.mu.Lock()
		link, ok := m.resets[email]
		delete(m.resets, email)
		m.mu.Unlock()
		if ok {
			return link
		}
	}
	t.Fatalf("no password reset link captured for %s", email)
	return ""
}

func linkToken(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil || u.Query().Get("token") == "" {
		t.Fatalf("no token in captured link %q", link)
	}
	return u.Query().Get("token")
}

type testServer struct {
	t      *testing.T
	srv    *httptest.Server
	mailer *captureMailer
}

func newTestServer(t *testing.T, mutate ...func(*config.Config)) *testServer {
	t.Helper()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	cfg := &config.Config{
		App:  config.AppConfig{BaseURL: "http://example.test"},
		Auth: config.AuthConfig{RateLimitPerMinute: 1000, RateLimitBurst: 1000},
	}
	for _, m := range mutate {
		m(cfg)
	}

	mailer := &captureMailer{links: map[string]string{}, resets: map[string]string{}, invites: map[string]string{}}
	tokens := security.NewJWT(testutil.NewRSAKey(t), time.Minute)
	mods := wireModules(testStorage(t), security.NewBcryptHasher(bcrypt.MinCost), tokens, mailers{identity: mailer, teams: mailer}, cfg)

	srv := httptest.NewServer(newRouter(cfg, mods))
	t.Cleanup(srv.Close)
	return &testServer{t: t, srv: srv, mailer: mailer}
}

// e2eBackendEnv picks the storage the end-to-end suite runs on: memory by
// default, or "postgres" / "mongo" on throwaway databases from dbtest (which
// also needs TEST_POSTGRES_URI / TEST_MONGO_URI). `make test-integration`
// runs the suite on all three.
const e2eBackendEnv = "TEST_E2E_BACKEND"

func testStorage(t *testing.T) *storage {
	t.Helper()
	switch backend := os.Getenv(e2eBackendEnv); backend {
	case "", "memory":
		return newMemoryStorage()
	case "postgres":
		return postgresStorage(dbtest.Postgres(t))
	case "mongo":
		return mongoStorage(context.Background(), dbtest.Mongo(t))
	default:
		t.Fatalf("%s=%q: want memory, postgres or mongo", e2eBackendEnv, backend)
		return nil
	}
}

type response struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func (s *testServer) do(method, path, token string, body any) response {
	s.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.srv.URL+path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := response{status: res.StatusCode, header: res.Header, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func (r response) expect(t *testing.T, status int) response {
	t.Helper()
	if r.status != status {
		t.Fatalf("status = %d, want %d; body: %s", r.status, status, r.raw)
	}
	return r
}

func (r response) expectCode(t *testing.T, status int, code string) {
	t.Helper()
	r.expect(t, status)
	if got := r.str("error", "code"); got != code {
		t.Fatalf("code = %q, want %s; body: %s", got, code, r.raw)
	}
	if r.str("error", "message") == "" {
		t.Fatalf("error message missing; body: %s", r.raw)
	}
}

func (r response) str(path ...string) string {
	var v any = r.body
	for _, p := range path {
		m, _ := v.(map[string]any)
		v = m[p]
	}
	s, _ := v.(string)
	return s
}

type session struct{ access, refresh, userID string }

func (s *testServer) register(name, email, password string) session {
	s.t.Helper()
	r := s.do("POST", "/auth/register", "", map[string]string{"name": name, "email": email, "password": password}).expect(s.t, 201)
	return session{access: r.str("authorization", "access_token"), refresh: r.str("authorization", "refresh_token"), userID: r.str("user", "id")}
}

func (s *testServer) login(email, password string) session {
	s.t.Helper()
	r := s.do("POST", "/auth/login", "", map[string]string{"email": email, "password": password}).expect(s.t, 200)
	return session{access: r.str("authorization", "access_token"), refresh: r.str("authorization", "refresh_token"), userID: r.str("user", "id")}
}

// ---- tests ----

func TestHealth(t *testing.T) {
	s := newTestServer(t)
	r := s.do("GET", "/health", "", nil).expect(t, 200)
	if r.raw != "ok" || r.header.Get("X-Request-ID") == "" {
		t.Fatalf("health = %q, request id %q", r.raw, r.header.Get("X-Request-ID"))
	}
}

func TestUnknownRoutesUseJSONErrors(t *testing.T) {
	s := newTestServer(t)
	s.do("GET", "/nope", "", nil).expectCode(t, 404, "NOT_FOUND")
	s.do("PATCH", "/todos", "", nil).expectCode(t, 405, "METHOD_NOT_ALLOWED")
}

func TestRegisterAndLogin(t *testing.T) {
	s := newTestServer(t)

	r := s.do("POST", "/auth/register", "", map[string]string{"name": " Ann ", "email": "Ann@Example.com", "password": "correct horse"}).expect(t, 201)
	if r.str("user", "email") != "ann@example.com" || r.str("user", "name") != "Ann" {
		t.Fatalf("user not normalized: %s", r.raw)
	}
	if r.body["user"].(map[string]any)["emailVerified"] != false {
		t.Fatalf("new user should be unverified: %s", r.raw)
	}
	if r.str("authorization", "token_type") != "Bearer" || r.str("authorization", "refresh_token") == "" {
		t.Fatalf("missing tokens: %s", r.raw)
	}
	if strings.Contains(r.raw, "password") {
		t.Fatal("response must never contain the password or its hash")
	}

	s.do("POST", "/auth/register", "", map[string]string{"name": "Ann", "email": "ann@example.com", "password": "another one"}).
		expectCode(t, 409, "EMAIL_TAKEN")

	s.login("ANN@example.com", "correct horse")
	s.do("POST", "/auth/login", "", map[string]string{"email": "ann@example.com", "password": "wrong password"}).
		expectCode(t, 401, "INVALID_CREDENTIALS")
	s.do("POST", "/auth/login", "", map[string]string{"email": "nobody@example.com", "password": "whatever1"}).
		expectCode(t, 401, "INVALID_CREDENTIALS")
}

func TestRegisterValidation(t *testing.T) {
	s := newTestServer(t)
	cases := []struct {
		body map[string]string
		code string
	}{
		{map[string]string{"name": "", "email": "a@b.co", "password": "longenough"}, "INVALID_NAME"},
		{map[string]string{"name": "A", "email": "nope", "password": "longenough"}, "INVALID_EMAIL"},
		{map[string]string{"name": "A", "email": "a@b.co", "password": "short"}, "WEAK_PASSWORD"},
		{map[string]string{"name": "A", "email": "a@b.co", "password": strings.Repeat("x", 73)}, "PASSWORD_TOO_LONG"},
	}
	for _, c := range cases {
		s.do("POST", "/auth/register", "", c.body).expectCode(t, 400, c.code)
	}
	s.do("POST", "/auth/register", "", nil).expectCode(t, 400, "INVALID_BODY")
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	s := newTestServer(t)
	first := s.register("Bo", "bo@example.com", "password1")

	r := s.do("POST", "/auth/refresh", "", map[string]string{"refresh_token": first.refresh}).expect(t, 200)
	second := r.str("refresh_token")
	if second == "" || second == first.refresh {
		t.Fatalf("refresh token was not rotated: %s", r.raw)
	}

	// Replaying the old token is treated as theft: the whole family is revoked.
	s.do("POST", "/auth/refresh", "", map[string]string{"refresh_token": first.refresh}).expectCode(t, 401, "REFRESH_TOKEN_REUSED")
	s.do("POST", "/auth/refresh", "", map[string]string{"refresh_token": second}).expectCode(t, 401, "REFRESH_TOKEN_REUSED")

	s.do("POST", "/auth/refresh", "", map[string]string{"refresh_token": "garbage"}).expectCode(t, 401, "INVALID_REFRESH_TOKEN")
	s.do("POST", "/auth/refresh", "", map[string]string{}).expectCode(t, 400, "INVALID_VALUE")
}

func TestLogoutRevokesOnlyThatLogin(t *testing.T) {
	s := newTestServer(t)
	phone := s.register("Cy", "cy@example.com", "password1")
	laptop := s.login("cy@example.com", "password1")

	s.do("POST", "/auth/logout", "", map[string]string{"refresh_token": phone.refresh}).expect(t, 204)
	s.do("POST", "/auth/logout", "", map[string]string{"refresh_token": phone.refresh}).expect(t, 204) // idempotent

	s.do("POST", "/auth/refresh", "", map[string]string{"refresh_token": phone.refresh}).expect(t, 401)
	s.do("POST", "/auth/refresh", "", map[string]string{"refresh_token": laptop.refresh}).expect(t, 200)
}

func TestLogoutAllRevokesAccessAndRefreshTokens(t *testing.T) {
	s := newTestServer(t)
	a := s.register("Di", "di@example.com", "password1")
	b := s.login("di@example.com", "password1")

	s.do("GET", "/todos", a.access, nil).expect(t, 200)
	s.do("POST", "/auth/logout-all", b.access, nil).expect(t, 204)

	s.do("GET", "/todos", a.access, nil).expectCode(t, 401, "TOKEN_REVOKED")
	s.do("GET", "/todos", b.access, nil).expectCode(t, 401, "TOKEN_REVOKED")
	s.do("POST", "/auth/refresh", "", map[string]string{"refresh_token": a.refresh}).expect(t, 401)

	fresh := s.login("di@example.com", "password1")
	s.do("GET", "/todos", fresh.access, nil).expect(t, 200)
}

func TestPasswordReset(t *testing.T) {
	s := newTestServer(t, func(c *config.Config) { c.Auth.PasswordResetURL = "https://app.example.test/reset" })
	old := s.register("Ro", "ro@example.com", "password1")

	// Same answer for unknown accounts, so the endpoint can't list users.
	s.do("POST", "/auth/password/forgot", "", map[string]string{"email": "nobody@example.com"}).expect(t, 202)
	s.do("POST", "/auth/password/forgot", "", map[string]string{"email": "ro@example.com"}).expect(t, 202)

	link := s.mailer.resetLink(t, "ro@example.com")
	if !strings.HasPrefix(link, "https://app.example.test/reset?token=") {
		t.Fatalf("reset link = %q, want it under AUTH_PASSWORD_RESET_URL", link)
	}
	token := linkToken(t, link)

	reset := func(token, password string) response {
		return s.do("POST", "/auth/password/reset", "", map[string]string{"token": token, "password": password})
	}
	reset(token, "short").expectCode(t, 400, "WEAK_PASSWORD")
	reset("nope", "brand new password").expectCode(t, 400, "INVALID_RESET_TOKEN")
	reset(token, "brand new password").expect(t, 204)
	reset(token, "brand new password").expectCode(t, 400, "INVALID_RESET_TOKEN")

	s.do("GET", "/me", old.access, nil).expectCode(t, 401, "TOKEN_REVOKED")
	s.do("POST", "/auth/refresh", "", map[string]string{"refresh_token": old.refresh}).expect(t, 401)
	s.do("POST", "/auth/login", "", map[string]string{"email": "ro@example.com", "password": "password1"}).expectCode(t, 401, "INVALID_CREDENTIALS")
	fresh := s.login("ro@example.com", "brand new password")
	if me := s.do("GET", "/me", fresh.access, nil).expect(t, 200); me.body["emailVerified"] != true {
		t.Fatal("a completed reset verifies the email address")
	}
}

func TestChangePassword(t *testing.T) {
	s := newTestServer(t)
	a := s.register("Tu", "tu@example.com", "password1")
	b := s.login("tu@example.com", "password1")

	change := func(access, current, next string) response {
		return s.do("POST", "/auth/password/change", access, map[string]string{"current_password": current, "new_password": next})
	}
	s.do("POST", "/auth/password/change", "", map[string]string{}).expectCode(t, 401, "MISSING_ACCESS_TOKEN")
	change(a.access, "wrong password", "brand new password").expectCode(t, 400, "INCORRECT_PASSWORD")
	change(a.access, "password1", "short").expectCode(t, 400, "WEAK_PASSWORD")

	r := change(a.access, "password1", "brand new password").expect(t, 200)
	access := r.str("authorization", "access_token")
	if access == "" || r.str("authorization", "refresh_token") == "" {
		t.Fatalf("change must return a fresh token pair: %s", r.raw)
	}
	s.do("GET", "/me", access, nil).expect(t, 200)
	s.do("GET", "/me", a.access, nil).expectCode(t, 401, "TOKEN_REVOKED")
	s.do("GET", "/me", b.access, nil).expectCode(t, 401, "TOKEN_REVOKED")
	s.do("POST", "/auth/refresh", "", map[string]string{"refresh_token": b.refresh}).expect(t, 401)
	s.login("tu@example.com", "brand new password")
}

func TestEmailVerification(t *testing.T) {
	s := newTestServer(t)
	u := s.register("Ed", "ed@example.com", "password1")

	first := s.mailer.token(t, "ed@example.com")
	s.do("POST", "/auth/verify-email/resend", u.access, nil).expect(t, 202)
	second := s.mailer.token(t, "ed@example.com")
	if first == second {
		t.Fatal("resend must issue a new token")
	}

	// The previous link is invalidated by the resend.
	s.do("GET", "/auth/verify-email?token="+url.QueryEscape(first), "", nil).expectCode(t, 400, "INVALID_VERIFICATION_TOKEN")
	s.do("GET", "/auth/verify-email?token="+url.QueryEscape(second), "", nil).expect(t, 200)
	// Single use.
	s.do("POST", "/auth/verify-email", "", map[string]string{"token": second}).expectCode(t, 400, "INVALID_VERIFICATION_TOKEN")

	me := s.do("GET", "/me", u.access, nil).expect(t, 200)
	if me.body["emailVerified"] != true {
		t.Fatalf("expected verified email: %s", me.raw)
	}
	s.do("POST", "/auth/verify-email/resend", u.access, nil).expectCode(t, 409, "EMAIL_ALREADY_VERIFIED")
}

func TestRequireVerifiedEmail(t *testing.T) {
	s := newTestServer(t, func(c *config.Config) { c.Auth.RequireVerifiedEmail = true })
	u := s.register("Fi", "fi@example.com", "password1")

	s.do("GET", "/todos", u.access, nil).expectCode(t, 403, "EMAIL_NOT_VERIFIED")
	s.do("GET", "/me", u.access, nil).expect(t, 200) // identity endpoints stay usable

	s.do("POST", "/auth/verify-email", "", map[string]string{"token": s.mailer.token(t, "fi@example.com")}).expect(t, 200)
	s.do("GET", "/todos", u.access, nil).expect(t, 200)
}

func TestTodosCRUDAndOwnership(t *testing.T) {
	s := newTestServer(t)
	owner := s.register("Gus", "gus@example.com", "password1")
	other := s.register("Hal", "hal@example.com", "password1")

	created := s.do("POST", "/todos", owner.access, map[string]string{"title": "  buy milk "}).expect(t, 201)
	id := created.str("id")
	if created.str("title") != "buy milk" || created.str("userId") != owner.userID {
		t.Fatalf("unexpected todo: %s", created.raw)
	}

	s.do("PUT", "/todos/"+id, owner.access, map[string]any{"completed": true}).expect(t, 200)
	got := s.do("GET", "/todos/"+id, owner.access, nil).expect(t, 200)
	if got.body["completed"] != true || got.str("title") != "buy milk" {
		t.Fatalf("partial update failed: %s", got.raw)
	}

	s.do("GET", "/todos/"+id, other.access, nil).expectCode(t, 404, "NOT_FOUND")
	s.do("PUT", "/todos/"+id, other.access, map[string]any{"title": "x"}).expect(t, 404)
	s.do("DELETE", "/todos/"+id, other.access, nil).expect(t, 404)

	s.do("PUT", "/todos/"+id, owner.access, map[string]any{"title": nil}).expectCode(t, 400, "INVALID_TITLE")
	s.do("PUT", "/todos/"+id, owner.access, map[string]any{}).expectCode(t, 400, "INVALID_BODY")
	s.do("POST", "/todos", owner.access, map[string]string{"title": strings.Repeat("x", 501)}).expectCode(t, 400, "TITLE_TOO_LONG")

	s.do("DELETE", "/todos/"+id, owner.access, nil).expect(t, 200)
	s.do("GET", "/todos/"+id, owner.access, nil).expect(t, 404)
}

func TestProtectedRoutesRequireToken(t *testing.T) {
	s := newTestServer(t)
	for _, route := range []struct{ method, path string }{
		{"GET", "/todos"}, {"POST", "/todos"}, {"GET", "/me"}, {"DELETE", "/me"},
		{"POST", "/auth/logout-all"}, {"POST", "/auth/verify-email/resend"},
	} {
		s.do(route.method, route.path, "", nil).expectCode(t, 401, "MISSING_ACCESS_TOKEN")
	}
	s.do("GET", "/todos", "not.a.jwt", nil).expectCode(t, 401, "MALFORMED_ACCESS_TOKEN")
}

func TestOverviewAndAccountDeletion(t *testing.T) {
	s := newTestServer(t)
	u := s.register("Ivy", "ivy@example.com", "password1")
	s.do("POST", "/todos", u.access, map[string]string{"title": "one"}).expect(t, 201)
	s.do("POST", "/todos", u.access, map[string]string{"title": "two"}).expect(t, 201)

	me := s.do("GET", "/me", u.access, nil).expect(t, 200)
	if todos, _ := me.body["todos"].([]any); len(todos) != 2 || me.str("email") != "ivy@example.com" {
		t.Fatalf("unexpected overview: %s", me.raw)
	}

	s.do("DELETE", "/me", u.access, nil).expect(t, 204)

	// Tokens of a deleted account stop working immediately.
	s.do("GET", "/me", u.access, nil).expectCode(t, 401, "INVALID_ACCESS_TOKEN")
	s.do("POST", "/auth/refresh", "", map[string]string{"refresh_token": u.refresh}).expect(t, 401)
	s.do("POST", "/auth/login", "", map[string]string{"email": "ivy@example.com", "password": "password1"}).expect(t, 401)

	// The email can be registered again, with no leftover todos.
	again := s.register("Ivy", "ivy@example.com", "password2")
	if todos := s.do("GET", "/todos", again.access, nil).expect(t, 200).raw; strings.TrimSpace(todos) != "[]" {
		t.Fatalf("leftover todos after deletion: %s", todos)
	}
}

func TestAuthRateLimit(t *testing.T) {
	s := newTestServer(t, func(c *config.Config) { c.Auth.RateLimitPerMinute, c.Auth.RateLimitBurst = 1, 3 })

	for range 3 {
		s.do("POST", "/auth/login", "", map[string]string{"email": "x@example.com", "password": "password1"}).expect(t, 401)
	}
	r := s.do("POST", "/auth/login", "", map[string]string{"email": "x@example.com", "password": "password1"})
	r.expectCode(t, 429, "RATE_LIMITED")
	if r.header.Get("Retry-After") == "" {
		t.Fatal("429 must carry Retry-After")
	}

	// Authenticated endpoints are not subject to the auth limiter.
	s.do("GET", "/health", "", nil).expect(t, 200)
}

func TestBodySizeLimit(t *testing.T) {
	s := newTestServer(t)
	huge := map[string]string{"name": strings.Repeat("a", 2<<20), "email": "a@b.co", "password": "password1"}
	s.do("POST", "/auth/register", "", huge).expectCode(t, 400, "INVALID_BODY")
}
