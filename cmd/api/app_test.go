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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
)

// captureMailer records invitation links instead of sending them.
type captureMailer struct {
	mu      sync.Mutex
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

func linkToken(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil || u.Query().Get("token") == "" {
		t.Fatalf("no token in captured link %q", link)
	}
	return u.Query().Get("token")
}

type testServer struct {
	t        *testing.T
	srv      *httptest.Server
	store    *storage
	mailer   *captureMailer
	firebase *fakeFirebase
}

// newTestServer runs the API on a fake Firebase. Options mutate the config;
// noServiceAccount runs it as if FIREBASE_CREDENTIALS_FILE were unset.
func newTestServer(t *testing.T, mutate ...func(*config.Config)) *testServer {
	t.Helper()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	cfg := &config.Config{
		App:      config.AppConfig{BaseURL: "http://example.test"},
		Auth:     config.AuthConfig{RateLimitPerMinute: 1000, RateLimitBurst: 1000},
		Firebase: config.FirebaseConfig{ProjectID: "todos-test", CredentialsFile: "service-account.json"},
	}
	for _, m := range mutate {
		m(cfg)
	}

	mailer := &captureMailer{invites: map[string]string{}}
	fb := newFakeFirebase()
	rp, err := newRelyingParty(cfg)
	if err != nil {
		t.Fatal(err)
	}
	in := signIn{idTokens: fb, passkeys: rp}
	if cfg.Firebase.CredentialsFile != "" {
		in.firebase = fb
	}
	store := testStorage(t)
	mods := wireModules(store, in, mailers{teams: mailer}, cfg)

	srv := httptest.NewServer(newRouter(cfg, mods))
	t.Cleanup(srv.Close)
	return &testServer{t: t, srv: srv, store: store, mailer: mailer, firebase: fb}
}

func noServiceAccount(c *config.Config) { c.Firebase.CredentialsFile = "" }

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

// session is a signed-in client: access is its Firebase ID token.
type session struct{ access, userID string }

// signInAs signs in to (fake) Firebase as the user with email and tells the
// API, which answers with status.
func (s *testServer) signInAs(name, email string, status int) session {
	s.t.Helper()
	tok := s.firebase.signIn(application.IDClaims{Email: email, EmailVerified: true, Name: name})
	r := s.do("POST", "/auth", tok, nil).expect(s.t, status)
	return session{access: tok, userID: r.str("user", "id")}
}

// register is a user's first sign-in: it creates the account.
func (s *testServer) register(name, email string) session { return s.signInAs(name, email, 201) }

// login is a later sign-in of an existing account.
func (s *testServer) login(email string) session { return s.signInAs("", email, 200) }

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

func TestFirebaseSignIn(t *testing.T) {
	s := newTestServer(t)

	tok := s.firebase.signIn(application.IDClaims{Email: "Ann@Example.com", Name: " Ann ", Provider: "google.com"})
	r := s.do("POST", "/auth", tok, nil).expect(t, 201)
	if r.str("user", "email") != "ann@example.com" || r.str("user", "name") != "Ann" || r.body["created"] != true {
		t.Fatalf("first sign-in: %s", r.raw)
	}
	if r.body["user"].(map[string]any)["emailVerified"] != false {
		t.Fatalf("an unverified Firebase email must stay unverified: %s", r.raw)
	}
	id := r.str("user", "id")
	if me := s.do("GET", "/me", tok, nil).expect(t, 200); me.str("id") != id {
		t.Fatalf("/me with the ID token: %s", me.raw)
	}

	// Signing in again (e.g. after verifying the email) finds the account.
	again := s.firebase.signIn(application.IDClaims{Email: "ann@example.com", EmailVerified: true, Provider: "password"})
	r = s.do("POST", "/auth", again, nil).expect(t, 200)
	if r.str("user", "id") != id || r.body["created"] != false || r.body["user"].(map[string]any)["emailVerified"] != true {
		t.Fatalf("second sign-in: %s", r.raw)
	}

	s.do("POST", "/auth", "", nil).expectCode(t, 401, "MISSING_ACCESS_TOKEN")
	// The token goes in the header; a body is ignored.
	s.do("POST", "/auth", "", map[string]string{"id_token": tok}).expectCode(t, 401, "MISSING_ACCESS_TOKEN")
	s.do("POST", "/auth", "forged", nil).expectCode(t, 401, "INVALID_ACCESS_TOKEN")
	noEmail := s.firebase.signIn(application.IDClaims{UID: "uid-phone", Provider: "phone"})
	s.do("POST", "/auth", noEmail, nil).expectCode(t, 400, "EMAIL_REQUIRED")
}

func TestFirebaseTokenNeedsAnAccount(t *testing.T) {
	s := newTestServer(t)
	tok := s.firebase.signIn(application.IDClaims{Email: "new@example.com", EmailVerified: true})
	s.do("GET", "/me", tok, nil).expectCode(t, 401, "ACCOUNT_NOT_REGISTERED")
	s.do("GET", "/todos", tok, nil).expectCode(t, 401, "ACCOUNT_NOT_REGISTERED")
}

func TestExistingAccountIsLinkedByVerifiedEmail(t *testing.T) {
	s := newTestServer(t)
	// An account from before Firebase: it has no Firebase user yet.
	legacy := &domain.User{Name: "Old", Email: "old@example.com", CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := s.store.users.Create(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}

	unverified := s.firebase.signIn(application.IDClaims{Email: "old@example.com"})
	s.do("POST", "/auth", unverified, nil).expectCode(t, 409, "EMAIL_NOT_VERIFIED")

	verified := s.firebase.signIn(application.IDClaims{Email: "OLD@example.com", EmailVerified: true, Provider: "google.com"})
	r := s.do("POST", "/auth", verified, nil).expect(t, 200)
	if r.str("user", "id") != legacy.ID || r.body["created"] != false {
		t.Fatalf("link: %s", r.raw)
	}
	s.do("GET", "/me", verified, nil).expect(t, 200)
	if got := actions(t, s.do("GET", "/me/audit-log", verified, nil).expect(t, 200)); !slices.Equal(got, []string{"account.linked"}) {
		t.Fatalf("security log = %q", got)
	}

	// Another Firebase user with the same, verified, email can't take it.
	impostor := s.firebase.signIn(application.IDClaims{UID: "uid-impostor", Email: "old@example.com", EmailVerified: true})
	s.do("POST", "/auth", impostor, nil).expectCode(t, 409, "EMAIL_TAKEN")
}

func TestLogoutAllRevokesEarlierSignIns(t *testing.T) {
	s := newTestServer(t)
	s.register("Ro", "ro@example.com")
	earlier := s.firebase.signIn(application.IDClaims{Email: "ro@example.com", EmailVerified: true, AuthTime: time.Now().Add(-time.Hour)})
	s.do("GET", "/me", earlier, nil).expect(t, 200)

	s.do("POST", "/auth/logout-all", earlier, nil).expect(t, 204)
	s.do("GET", "/me", earlier, nil).expectCode(t, 401, "TOKEN_REVOKED")
	s.do("POST", "/auth", earlier, nil).expectCode(t, 401, "TOKEN_REVOKED")

	fresh := s.login("ro@example.com")
	s.do("GET", "/me", fresh.access, nil).expect(t, 200)
}

func TestRequireVerifiedEmail(t *testing.T) {
	s := newTestServer(t, func(c *config.Config) { c.Auth.RequireVerifiedEmail = true })
	tok := s.firebase.signIn(application.IDClaims{Email: "fi@example.com"})
	s.do("POST", "/auth", tok, nil).expect(t, 201)

	s.do("GET", "/todos", tok, nil).expectCode(t, 403, "EMAIL_NOT_VERIFIED")
	s.do("GET", "/me", tok, nil).expect(t, 200) // identity endpoints stay usable

	// Verified in Firebase: the next ID token says so.
	verified := s.firebase.signIn(application.IDClaims{Email: "fi@example.com", EmailVerified: true})
	s.do("GET", "/todos", verified, nil).expect(t, 200)
}

func TestRemovedPasswordEndpoints(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/auth/firebase", "/auth/register", "/auth/login", "/auth/refresh", "/auth/logout", "/auth/verify-email", "/auth/password/forgot", "/auth/password/reset", "/auth/password/change"} {
		s.do("POST", path, "", map[string]string{}).expectCode(t, 404, "NOT_FOUND")
	}
}

func TestTodosCRUDAndOwnership(t *testing.T) {
	s := newTestServer(t)
	owner := s.register("Gus", "gus@example.com")
	other := s.register("Hal", "hal@example.com")

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
		{"POST", "/auth/logout-all"}, {"GET", "/me/passkeys"},
	} {
		s.do(route.method, route.path, "", nil).expectCode(t, 401, "MISSING_ACCESS_TOKEN")
	}
	s.do("GET", "/todos", "not.a.firebase.token", nil).expectCode(t, 401, "INVALID_ACCESS_TOKEN")
}

func TestOverviewAndAccountDeletion(t *testing.T) {
	s := newTestServer(t)
	u := s.register("Ivy", "ivy@example.com")
	s.do("POST", "/todos", u.access, map[string]string{"title": "one"}).expect(t, 201)
	s.do("POST", "/todos", u.access, map[string]string{"title": "two"}).expect(t, 201)

	me := s.do("GET", "/me", u.access, nil).expect(t, 200)
	if todos, _ := me.body["todos"].([]any); len(todos) != 2 || me.str("email") != "ivy@example.com" {
		t.Fatalf("unexpected overview: %s", me.raw)
	}

	s.do("DELETE", "/me", u.access, nil).expect(t, 204)

	// The ID token stops working immediately, and the Firebase user is deleted.
	s.do("GET", "/me", u.access, nil).expectCode(t, 401, "ACCOUNT_NOT_REGISTERED")
	if len(s.firebase.deleted) != 1 || s.firebase.deleted[0] != uidOf("ivy@example.com") {
		t.Fatalf("Firebase users deleted: %v", s.firebase.deleted)
	}

	// Signing up again starts a fresh account, with no leftover todos.
	again := s.register("Ivy", "ivy@example.com")
	if todos := s.do("GET", "/todos", again.access, nil).expect(t, 200).raw; strings.TrimSpace(todos) != "[]" {
		t.Fatalf("leftover todos after deletion: %s", todos)
	}
}

func TestAuthRateLimit(t *testing.T) {
	s := newTestServer(t, func(c *config.Config) { c.Auth.RateLimitPerMinute, c.Auth.RateLimitBurst = 1, 3 })

	for range 3 {
		s.do("POST", "/auth", "forged", nil).expect(t, 401)
	}
	r := s.do("POST", "/auth", "forged", nil)
	r.expectCode(t, 429, "RATE_LIMITED")
	if r.header.Get("Retry-After") == "" {
		t.Fatal("429 must carry Retry-After")
	}

	// Authenticated endpoints are not subject to the auth limiter.
	s.do("GET", "/health", "", nil).expect(t, 200)
}

func TestBodySizeLimit(t *testing.T) {
	s := newTestServer(t)
	huge := map[string]string{"token": strings.Repeat("a", 2<<20)}
	s.do("POST", "/auth/passkeys/login", "", huge).expectCode(t, 400, "INVALID_BODY")
}
