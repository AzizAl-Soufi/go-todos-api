package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateRedirectURI(t *testing.T) {
	for uri, ok := range map[string]bool{
		"https://claude.ai/api/mcp/auth_callback": true,
		"http://localhost/callback":               true,
		"http://127.0.0.1:3118/callback":          true,
		"http://[::1]/cb":                         true,
		"http://example.com/cb":                   false, // http off loopback
		"https://app.example/cb#frag":             false,
		"https://user:pw@app.example/cb":          false,
		"/relative":                               false,
		"javascript:alert(1)":                     false,
		"custom-scheme://cb":                      false,
		"":                                        false,
		"https://a.example/" + strings.Repeat("x", MaxRedirectURILen): false,
	} {
		if err := ValidateRedirectURI(uri); (err == nil) != ok {
			t.Errorf("ValidateRedirectURI(%q) = %v, want ok %v", uri, err, ok)
		}
	}
}

func TestClientAllowsRedirect(t *testing.T) {
	c := &Client{RedirectURIs: []string{
		"https://claude.ai/api/mcp/auth_callback",
		"http://localhost/callback",
		"http://127.0.0.1/callback",
	}}
	for uri, ok := range map[string]bool{
		"https://claude.ai/api/mcp/auth_callback":      true,
		"http://localhost:3118/callback":               true, // any port on loopback
		"http://127.0.0.1:50000/callback":              true,
		"http://localhost/callback":                    true,
		"https://claude.ai/api/mcp/auth_callback?x=1":  false,
		"https://claude.ai:8443/api/mcp/auth_callback": false, // only loopback ignores the port
		"http://localhost:3118/other":                  false,
		"http://localhost.evil.example/callback":       false,
		"http://[::1]:3118/callback":                   false, // not registered
		"https://evil.example/api/mcp/auth_callback":   false,
	} {
		if got := c.AllowsRedirect(uri); got != ok {
			t.Errorf("AllowsRedirect(%q) = %v, want %v", uri, got, ok)
		}
	}
	if c.OnlyLoopback() {
		t.Fatal("a client with an https redirect is not loopback-only")
	}
	if !(&Client{RedirectURIs: []string{"http://localhost/cb"}}).OnlyLoopback() {
		t.Fatal("loopback-only client not detected")
	}
}

func TestNewClient(t *testing.T) {
	now := time.Now()
	c, err := NewClient("  ", []string{"https://app.example/cb"}, Registered, now)
	if err != nil || c.Name != "MCP client" {
		t.Fatalf("NewClient = %+v (%v), want a default name", c, err)
	}
	if _, err := NewClient("x", nil, Registered, now); !errors.Is(err, ErrInvalidRedirectURI) {
		t.Fatalf("no redirect URIs: %v", err)
	}
	if _, err := NewClient(strings.Repeat("n", MaxClientNameLength+1), []string{"https://a.example/cb"}, Registered, now); !errors.Is(err, ErrInvalidClientMetadata) {
		t.Fatalf("long name: %v", err)
	}
}

func TestPKCE(t *testing.T) {
	// challenge = base64url(SHA-256(verifier)), computed independently.
	verifier := "dBjftJeZ4CVP-mJ92K9cX5tRjE6g1nq3ypMPyQKmqZw"
	challenge := "5B8xNdcE3ePhVz8hJEPbPm0zHk-EkA5LHZNaxrm53Ic"
	if err := ValidateCodeChallenge(challenge, "S256"); err != nil {
		t.Fatal(err)
	}
	if !VerifyPKCE(challenge, verifier) {
		t.Fatal("a matching verifier does not verify")
	}
	for _, bad := range []string{"", verifier[:42], verifier + "!", strings.Repeat("a", 129), strings.Replace(verifier, "d", "e", 1)} {
		if VerifyPKCE(challenge, bad) {
			t.Errorf("verifier %q accepted", bad)
		}
	}
	if err := ValidateCodeChallenge(challenge, "plain"); !errors.Is(err, ErrInvalidPKCE) {
		t.Fatalf("plain method: %v", err)
	}
	if err := ValidateCodeChallenge("short", "S256"); !errors.Is(err, ErrInvalidPKCE) {
		t.Fatalf("short challenge: %v", err)
	}
}

func TestParseScope(t *testing.T) {
	for raw, want := range map[string]string{
		"":                     Scope,
		"todos":                Scope,
		"todos offline_access": Scope + " " + OfflineAccess,
		" offline_access  ":    Scope + " " + OfflineAccess,
	} {
		if got, err := ParseScope(raw); err != nil || got != want {
			t.Errorf("ParseScope(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if _, err := ParseScope("todos admin"); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("unknown scope: %v", err)
	}
}

func TestTokens(t *testing.T) {
	tok := FormatToken(AccessToken, "grant-1", "c2VjcmV0_-x")
	if id, secret, ok := ParseToken(AccessToken, tok); !ok || id != "grant-1" || secret != "c2VjcmV0_-x" {
		t.Fatalf("ParseToken(%q) = %q, %q, %v", tok, id, secret, ok)
	}
	for _, bad := range []string{"", tok + ".x", "mcpr.grant-1.secret", "mcpa..secret", "mcpa.grant-1.", "Bearer x"} {
		if _, _, ok := ParseToken(AccessToken, bad); ok {
			t.Errorf("ParseToken(%q) accepted", bad)
		}
	}
}

func TestAuthorizationRequestCheck(t *testing.T) {
	const resource = "https://api.example.com/mcp"
	ok := AuthorizationRequest{
		ResponseType: "code", CodeChallenge: "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", CodeChallengeMethod: "S256",
	}
	got, err := ok.Check(resource)
	if err != nil || got.Scope != Scope || got.Resource != resource {
		t.Fatalf("Check = %+v (%v), want defaults filled in", got, err)
	}
	withSlash := ok
	withSlash.Resource = resource + "/"
	if _, err := withSlash.Check(resource); err != nil {
		t.Fatalf("trailing slash: %v", err)
	}
	for name, c := range map[string]struct {
		mutate func(*AuthorizationRequest)
		want   error
	}{
		"token response type": {func(r *AuthorizationRequest) { r.ResponseType = "token" }, ErrUnsupportedResponseType},
		"no PKCE":             {func(r *AuthorizationRequest) { r.CodeChallenge = "" }, ErrInvalidPKCE},
		"bad scope":           {func(r *AuthorizationRequest) { r.Scope = "admin" }, ErrInvalidScope},
		"other resource":      {func(r *AuthorizationRequest) { r.Resource = "https://evil.example/mcp" }, ErrInvalidTarget},
	} {
		r := ok
		c.mutate(&r)
		if _, err := r.Check(resource); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}
