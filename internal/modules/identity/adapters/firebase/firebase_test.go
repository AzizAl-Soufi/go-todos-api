package firebase_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/firebase"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
)

const project = "todos-test"

// In emulator mode the SDK checks a token's project, issuer and times but
// not its signature, so these tests can mint tokens that fail those checks.
// (A token that passes them is then checked against a running emulator,
// which these tests don't start.)
func newClient(t *testing.T) *firebase.Client {
	t.Helper()
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "localhost:9099")
	c, err := firebase.New(context.Background(), project, "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func idToken(t *testing.T, mutate func(claims map[string]any)) string {
	t.Helper()
	now := time.Now()
	claims := map[string]any{
		"iss": "https://securetoken.google.com/" + project, "aud": project,
		"sub": "uid-1", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "auth_time": now.Add(-time.Minute).Unix(),
		"email": "ann@example.com", "email_verified": true, "name": "Ann",
		"firebase": map[string]any{"sign_in_provider": "google.com"},
	}
	if mutate != nil {
		mutate(claims)
	}
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]string{"alg": "none", "typ": "JWT"}) + "." + enc(claims) + "."
}

func TestVerifyIDToken_Rejects(t *testing.T) {
	c := newClient(t)
	cases := map[string]struct {
		token string
		want  error
	}{
		"empty":         {"", domain.ErrInvalidIDToken},
		"garbage":       {"not.a.token", domain.ErrInvalidIDToken},
		"other project": {idToken(t, func(c map[string]any) { c["aud"] = "someone-else" }), domain.ErrInvalidIDToken},
		"wrong issuer":  {idToken(t, func(c map[string]any) { c["iss"] = "https://evil.test" }), domain.ErrInvalidIDToken},
		"expired": {idToken(t, func(c map[string]any) {
			c["iat"], c["exp"] = time.Now().Add(-2*time.Hour).Unix(), time.Now().Add(-time.Hour).Unix()
		}), domain.ErrExpiredIDToken},
	}
	for name, tc := range cases {
		if _, err := c.VerifyIDToken(context.Background(), tc.token); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

func TestNew_VerifiesWithoutCredentials(t *testing.T) {
	// Only the project ID is needed to verify ID tokens; construction must
	// not reach for a service account or the network.
	done := make(chan error, 1)
	go func() { _, err := firebase.New(context.Background(), project, ""); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("New blocked")
	}
}

func TestNew_RequiresProject(t *testing.T) {
	if _, err := firebase.New(context.Background(), "", ""); err == nil {
		t.Fatal("want an error without a project ID")
	}
}
