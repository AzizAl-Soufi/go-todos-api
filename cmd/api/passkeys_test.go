package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/passkeys/passkeytest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
)

// options returns the "options" object of a passkey options response.
func (r response) options(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(r.body["options"])
	if err != nil || r.str("token") == "" {
		t.Fatalf("no options or token: %s", r.raw)
	}
	return b
}

// addPasskey registers a passkey made by a on the account of sess.
func (s *testServer) addPasskey(a *passkeytest.Authenticator, sess session, name string) (string, *passkeytest.Credential) {
	s.t.Helper()
	opts := s.do("POST", "/me/passkeys/options", sess.access, nil).expect(s.t, 200)
	resp, cred := a.Create(s.t, opts.options(s.t))
	r := s.do("POST", "/me/passkeys", sess.access, map[string]any{"token": opts.str("token"), "name": name, "credential": resp}).expect(s.t, 201)
	return r.str("id"), cred
}

// passkeyLogin signs in with cred, answered by a.
func (s *testServer) passkeyLogin(a *passkeytest.Authenticator, cred *passkeytest.Credential) response {
	s.t.Helper()
	opts := s.do("POST", "/auth/passkeys/login/options", "", nil).expect(s.t, 200)
	return s.do("POST", "/auth/passkeys/login", "", map[string]any{"token": opts.str("token"), "credential": a.Get(s.t, opts.options(s.t), cred)})
}

func TestPasskeys(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com", "password1")
	bo := s.register("Bo", "bo@example.com", "password1")
	a := passkeytest.New("http://example.test") // APP_BASE_URL's origin

	s.do("POST", "/me/passkeys/options", "", nil).expectCode(t, 401, "MISSING_ACCESS_TOKEN")
	s.do("GET", "/me/passkeys", "", nil).expectCode(t, 401, "MISSING_ACCESS_TOKEN")
	if got := s.do("GET", "/me/passkeys", ann.access, nil).expect(t, 200).items(t); len(got) != 0 {
		t.Fatalf("new account has passkeys: %v", got)
	}

	opts := s.do("POST", "/me/passkeys/options", ann.access, nil).expect(t, 200)
	pk := opts.body["options"].(map[string]any)["publicKey"].(map[string]any)
	if pk["rp"].(map[string]any)["id"] != "example.test" || pk["user"].(map[string]any)["name"] != "ann@example.com" {
		t.Fatalf("creation options: %s", opts.raw)
	}
	resp, cred := a.Create(t, opts.options(t))
	s.do("POST", "/me/passkeys", ann.access, map[string]any{"token": opts.str("token")}).expectCode(t, 400, "INVALID_VALUE")
	created := s.do("POST", "/me/passkeys", ann.access, map[string]any{"token": opts.str("token"), "name": "Laptop", "credential": resp}).expect(t, 201)
	id := created.str("id")
	if created.str("name") != "Laptop" || created.str("createdAt") == "" || created.body["lastUsedAt"] != nil {
		t.Fatalf("created passkey: %s", created.raw)
	}
	s.do("POST", "/me/passkeys", ann.access, map[string]any{"token": opts.str("token"), "credential": resp}).
		expectCode(t, 400, "INVALID_PASSKEY_CHALLENGE")

	// Sign in without email or password.
	r := s.passkeyLogin(a, cred).expect(t, 200)
	if r.str("user", "id") != ann.userID || r.str("authorization", "refresh_token") == "" {
		t.Fatalf("passkey login: %s", r.raw)
	}
	access := r.str("authorization", "access_token")
	s.do("GET", "/me", access, nil).expect(t, 200)
	list := s.do("GET", "/me/passkeys", access, nil).expect(t, 200).items(t)
	if len(list) != 1 || list[0]["id"] != id || list[0]["lastUsedAt"] == nil {
		t.Fatalf("passkeys after sign-in: %v", list)
	}
	if raw := s.do("GET", "/me/passkeys", access, nil).raw; strings.Contains(raw, "credential") || strings.Contains(raw, "publicKey") {
		t.Fatalf("the credential record must not be exposed: %s", raw)
	}

	// A page on another origin can't use it, and the attempt is logged.
	s.passkeyLogin(passkeytest.New("http://evil.test"), cred).expectCode(t, 401, "INVALID_PASSKEY")
	got := actions(t, s.do("GET", "/me/audit-log", access, nil).expect(t, 200))
	if want := []string{"session.login_failed", "session.login", "passkey.added", "user.registered"}; !slices.Equal(got, want) {
		t.Fatalf("security log = %q, want %q", got, want)
	}

	// Only the owner can remove it; then it no longer signs in.
	s.do("DELETE", "/me/passkeys/"+id, bo.access, nil).expectCode(t, 404, "PASSKEY_NOT_FOUND")
	s.do("DELETE", "/me/passkeys/"+id, ann.access, nil).expect(t, 204)
	s.do("DELETE", "/me/passkeys/"+id, ann.access, nil).expectCode(t, 404, "PASSKEY_NOT_FOUND")
	s.passkeyLogin(a, cred).expectCode(t, 401, "INVALID_PASSKEY")
}

func TestPasskeyLoginValidation(t *testing.T) {
	s := newTestServer(t)
	s.do("POST", "/auth/passkeys/login", "", map[string]any{"token": "x"}).expectCode(t, 400, "INVALID_VALUE")
	s.do("POST", "/auth/passkeys/login", "", map[string]any{"token": "nope", "credential": map[string]any{}}).
		expectCode(t, 400, "INVALID_PASSKEY_CHALLENGE")
}

func TestPasskeysForAFrontendOrigin(t *testing.T) {
	s := newTestServer(t, func(c *config.Config) {
		c.Passkey.RPID = "example.test"
		c.Passkey.Origins = "https://app.example.test, https://admin.example.test"
	})
	ann := s.register("Ann", "ann@example.com", "password1")
	app := passkeytest.New("https://app.example.test")
	_, cred := s.addPasskey(app, ann, "")
	s.passkeyLogin(passkeytest.New("https://admin.example.test"), cred).expect(t, 200)
	s.passkeyLogin(passkeytest.New("http://example.test"), cred).expectCode(t, 401, "INVALID_PASSKEY")
}

func TestAccountDeletionRemovesPasskeys(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com", "password1")
	a := passkeytest.New("http://example.test")
	_, cred := s.addPasskey(a, ann, "")
	s.do("DELETE", "/me", ann.access, nil).expect(t, 204)
	s.passkeyLogin(a, cred).expectCode(t, 401, "INVALID_PASSKEY")
}
