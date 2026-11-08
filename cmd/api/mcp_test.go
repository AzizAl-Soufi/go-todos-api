package main

// End-to-end tests for the MCP endpoint and its OAuth 2.1 authorization
// server: discovery, client registration, sign-in and consent, the token
// endpoint, revocation, and tools called through the MCP Go SDK's client.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	oauthdomain "github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
)

const (
	baseURL      = "http://example.test" // APP_BASE_URL of the test server
	claudeHook   = "https://claude.ai/api/mcp/auth_callback"
	codeClaude   = "https://claude.ai/oauth/claude-code-client-metadata"
	pkceVerifier = "dBjftJeZ4CVP-mJ92K9cX5tRjE6g1nq3ypMPyQKmqZw"
)

// metadataDocs stands in for fetching Client ID Metadata Documents.
var metadataDocs = fakeMetadata{
	codeClaude: {ID: codeClaude, Name: "Claude Code", Kind: oauthdomain.MetadataDocument,
		RedirectURIs: []string{"http://localhost/callback", "http://127.0.0.1/callback"}},
}

type fakeMetadata map[string]oauthdomain.Client

func (f fakeMetadata) Fetch(_ context.Context, id string) (*oauthdomain.Client, error) {
	c, ok := f[id]
	if !ok {
		return nil, oauthdomain.ErrInvalidClient
	}
	return &c, nil
}

func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// noRedirects is an HTTP client that shows redirects instead of following them.
var noRedirects = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// registerClient registers an MCP client with DCR and returns its ID.
func (s *testServer) registerClient(redirect string) string {
	s.t.Helper()
	r := s.do("POST", "/oauth/register", "", map[string]any{
		"client_name": "Claude", "redirect_uris": []string{redirect}, "token_endpoint_auth_method": "none",
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
	}).expect(s.t, 201)
	if r.str("client_id") == "" || r.str("token_endpoint_auth_method") != "none" {
		s.t.Fatalf("registration: %s", r.raw)
	}
	return r.str("client_id")
}

func authorizeParams(clientID, redirect string) url.Values {
	return url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {"xyz"},
		"code_challenge": {challenge(pkceVerifier)}, "code_challenge_method": {"S256"},
		"resource": {baseURL + "/mcp"}, "scope": {"todos offline_access"},
	}
}

// approve posts the consent form as the signed-in user and returns where
// the browser is sent.
func (s *testServer) approve(params url.Values, idToken, decision string) *url.URL {
	s.t.Helper()
	form := url.Values{}
	for k, v := range params {
		form[k] = v
	}
	form.Set("id_token", idToken)
	form.Set("decision", decision)
	res, err := noRedirects.PostForm(s.srv.URL+"/oauth/authorize", form)
	if err != nil {
		s.t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		s.t.Fatalf("authorize decision: status %d", res.StatusCode)
	}
	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		s.t.Fatal(err)
	}
	return loc
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

func (s *testServer) token(form url.Values) (int, tokenResponse) {
	s.t.Helper()
	res, err := http.PostForm(s.srv.URL+"/oauth/token", form)
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Cache-Control") != "no-store" {
		s.t.Fatalf("token response must not be cached: %q", res.Header.Get("Cache-Control"))
	}
	var tr tokenResponse
	if err := json.NewDecoder(res.Body).Decode(&tr); err != nil {
		s.t.Fatal(err)
	}
	return res.StatusCode, tr
}

// connect runs the whole OAuth flow for sess and returns the tokens.
func (s *testServer) connect(sess session) (clientID string, tokens tokenResponse) {
	s.t.Helper()
	clientID = s.registerClient(claudeHook)
	loc := s.approve(authorizeParams(clientID, claudeHook), sess.access, "approve")
	status, tokens := s.token(url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {loc.Query().Get("code")},
		"redirect_uri": {claudeHook}, "code_verifier": {pkceVerifier}, "resource": {baseURL + "/mcp"},
	})
	if status != 200 || tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.TokenType != "Bearer" {
		s.t.Fatalf("code exchange: %d %+v", status, tokens)
	}
	return clientID, tokens
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// mcpSession connects the MCP Go SDK's client to /mcp with an access token.
func (s *testServer) mcpSession(accessToken string) *mcp.ClientSession {
	s.t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   s.srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{accessToken}},
		MaxRetries: -1,
	}, nil)
	if err != nil {
		s.t.Fatalf("connect: %v", err)
	}
	s.t.Cleanup(func() { cs.Close() })
	return cs
}

// call runs a tool and returns its structured result; tool errors fail the
// test unless wantErr is given, which the error text must contain.
func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any, wantErr ...string) map[string]any {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if len(wantErr) > 0 {
		if !res.IsError || !strings.Contains(text, wantErr[0]) {
			t.Fatalf("%s: want an error with %q, got isError=%v %s", tool, wantErr[0], res.IsError, text)
		}
		return nil
	}
	if res.IsError {
		t.Fatalf("%s failed: %s", tool, text)
	}
	out, _ := res.StructuredContent.(map[string]any)
	if out == nil {
		t.Fatalf("%s: no structured content: %s", tool, text)
	}
	return out
}

func TestMCPDiscovery(t *testing.T) {
	s := newTestServer(t)

	res, err := http.Post(s.srv.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	auth := res.Header.Get("WWW-Authenticate")
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(auth, `resource_metadata="`+baseURL+`/.well-known/oauth-protected-resource"`) ||
		!strings.Contains(auth, `scope="todos"`) {
		t.Fatalf("unauthenticated /mcp: %d, WWW-Authenticate %q", res.StatusCode, auth)
	}

	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		prm := s.do("GET", path, "", nil).expect(t, 200)
		if prm.str("resource") != baseURL+"/mcp" || prm.body["authorization_servers"].([]any)[0] != baseURL {
			t.Fatalf("%s: %s", path, prm.raw)
		}
	}
	as := s.do("GET", "/.well-known/oauth-authorization-server", "", nil).expect(t, 200)
	if as.str("issuer") != baseURL || as.str("token_endpoint") != baseURL+"/oauth/token" ||
		as.body["client_id_metadata_document_supported"] != true ||
		!slices.Contains(as.body["token_endpoint_auth_methods_supported"].([]any), any("none")) ||
		!slices.Contains(as.body["code_challenge_methods_supported"].([]any), any("S256")) {
		t.Fatalf("authorization server metadata: %s", as.raw)
	}
}

func TestMCPDisabledWithoutWebConfig(t *testing.T) {
	s := newTestServer(t, func(c *config.Config) { c.Firebase.WebAPIKey = "" })
	s.do("GET", "/.well-known/oauth-authorization-server", "", nil).expect(t, 404)
	s.do("POST", "/mcp", "", map[string]any{}).expect(t, 404)
}

func TestMCPToolsOverOAuth(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com")
	_, tokens := s.connect(ann)
	if tokens.ExpiresIn != 3600 || !strings.Contains(tokens.Scope, "todos") {
		t.Fatalf("tokens: %+v", tokens)
	}
	cs := s.mcpSession(tokens.AccessToken)

	list, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range list.Tools {
		byName[tool.Name] = tool
		if tool.Annotations == nil || tool.Annotations.Title == "" || tool.Annotations.DestructiveHint == nil {
			t.Errorf("%s: every tool needs a title and destructiveHint", tool.Name)
		}
	}
	for _, name := range []string{"get_profile", "list_todos", "create_todo", "delete_project", "list_teams", "invite_member", "get_security_log"} {
		if byName[name] == nil {
			t.Errorf("tool %s missing", name)
		}
	}
	for _, name := range []string{"delete_account", "logout_all", "delete_passkey"} {
		if byName[name] != nil {
			t.Errorf("account-level tool %s must not be exposed", name)
		}
	}
	if a := byName["delete_todo"].Annotations; !*a.DestructiveHint || a.ReadOnlyHint {
		t.Errorf("delete_todo annotations: %+v", a)
	}
	if a := byName["list_todos"].Annotations; !a.ReadOnlyHint {
		t.Errorf("list_todos annotations: %+v", a)
	}

	if p := call(t, cs, "get_profile", nil); p["email"] != "ann@example.com" || p["id"] != ann.userID {
		t.Fatalf("profile: %v", p)
	}

	// Tools and REST work on the same data, with the same rules.
	project := call(t, cs, "create_project", map[string]any{"name": "Launch", "color": "blue"})
	todo := call(t, cs, "create_todo", map[string]any{"title": "Ship it", "projectId": project["id"], "priority": "high", "dueAt": "2030-01-02"})
	if todo["title"] != "Ship it" || todo["priority"] != "high" || todo["projectId"] != project["id"] {
		t.Fatalf("created todo: %v", todo)
	}
	if rest := s.do("GET", "/todos/"+todo["id"].(string), ann.access, nil).expect(t, 200); rest.str("title") != "Ship it" {
		t.Fatalf("REST doesn't see the tool's todo: %s", rest.raw)
	}
	updated := call(t, cs, "update_todo", map[string]any{"id": todo["id"], "status": "done", "clearDueAt": true})
	if updated["status"] != "done" || updated["dueAt"] != nil || updated["completed"] != true {
		t.Fatalf("updated todo: %v", updated)
	}
	listed := call(t, cs, "list_todos", map[string]any{"status": []string{"done"}})
	if listed["total"] != float64(1) || listed["truncated"] != false {
		t.Fatalf("list_todos: %v", listed)
	}
	call(t, cs, "list_projects", nil)
	call(t, cs, "list_collections", nil)
	team := call(t, cs, "create_team", map[string]any{"name": "Platform"})
	if teams := call(t, cs, "list_teams", nil); len(teams["teams"].([]any)) != 1 {
		t.Fatalf("list_teams: %v", teams)
	}
	call(t, cs, "get_team", map[string]any{"teamId": team["id"]})
	call(t, cs, "get_team_audit_log", map[string]any{"teamId": team["id"]})
	call(t, cs, "get_security_log", nil)

	// Deleting a team returns an operation; the worker finishes it.
	deleted := call(t, cs, "delete_team", map[string]any{"teamId": team["id"]})
	op := deleted["operation"].(map[string]any)
	if op["status"] != "pending" {
		t.Fatalf("delete_team: %v", deleted)
	}
	s.runJobs()
	if got := call(t, cs, "get_operation", map[string]any{"id": op["id"]}); got["status"] != "succeeded" {
		t.Fatalf("get_operation after the worker ran: %v", got)
	}

	// Errors come back as tool errors with the API's codes.
	call(t, cs, "create_todo", map[string]any{"title": "x", "priority": "extreme"}, "INVALID_PRIORITY")
	call(t, cs, "delete_todo", map[string]any{"id": todo["id"]})
	call(t, cs, "get_todo", map[string]any{"id": todo["id"]}, "NOT_FOUND")
}

func TestOAuthRefreshRotationAndRevocation(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com")
	clientID, first := s.connect(ann)

	// Refreshing rotates both tokens; the old access token stops working.
	status, second := s.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {first.RefreshToken}})
	if status != 200 || second.RefreshToken == first.RefreshToken || second.AccessToken == first.AccessToken {
		t.Fatalf("refresh: %d %+v", status, second)
	}
	s.do("POST", "/mcp", first.AccessToken, map[string]any{}).expect(t, 401)
	s.mcpSession(second.AccessToken)

	// Replaying the old refresh token revokes the whole grant.
	status, replay := s.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {first.RefreshToken}})
	if status != 400 || replay.Error != "invalid_grant" {
		t.Fatalf("replayed refresh token: %d %+v", status, replay)
	}
	s.do("POST", "/mcp", second.AccessToken, map[string]any{}).expect(t, 401)
	if status, _ := s.token(url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {second.RefreshToken}}); status != 400 {
		t.Fatal("the grant must be gone after a replay")
	}

	// The user sees and disconnects apps; log out everywhere disconnects all.
	_, third := s.connect(ann)
	conns := s.do("GET", "/me/connections", ann.access, nil).expect(t, 200).items(t)
	if len(conns) != 1 || conns[0]["clientName"] != "Claude" {
		t.Fatalf("connections: %v", conns)
	}
	s.do("DELETE", "/me/connections/"+conns[0]["id"].(string), ann.access, nil).expect(t, 204)
	s.do("POST", "/mcp", third.AccessToken, map[string]any{}).expect(t, 401)

	_, fourth := s.connect(ann)
	s.do("POST", "/auth/logout-all", ann.access, nil).expect(t, 204)
	s.do("POST", "/mcp", fourth.AccessToken, map[string]any{}).expect(t, 401)

	// RFC 7009 revocation.
	ann = s.login("ann@example.com")
	clientID, fifth := s.connect(ann)
	res, err := http.PostForm(s.srv.URL+"/oauth/revoke", url.Values{"token": {fifth.RefreshToken}, "client_id": {clientID}})
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("revoke: %v %v", res, err)
	}
	s.do("POST", "/mcp", fifth.AccessToken, map[string]any{}).expect(t, 401)
}

func TestOAuthCodeChecks(t *testing.T) {
	s := newTestServer(t)
	ann := s.register("Ann", "ann@example.com")
	clientID := s.registerClient(claudeHook)
	params := authorizeParams(clientID, claudeHook)

	loc := s.approve(params, ann.access, "approve")
	if loc.Scheme+"://"+loc.Host+loc.Path != claudeHook || loc.Query().Get("state") != "xyz" || loc.Query().Get("iss") != baseURL {
		t.Fatalf("redirect: %s", loc)
	}
	code := loc.Query().Get("code")
	exchange := func(verifier string) (int, tokenResponse) {
		return s.token(url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code},
			"redirect_uri": {claudeHook}, "code_verifier": {verifier}})
	}
	// A wrong PKCE verifier fails, and uses up the code.
	if status, tr := exchange(strings.Repeat("x", 43)); status != 400 || tr.Error != "invalid_grant" {
		t.Fatalf("wrong verifier: %d %+v", status, tr)
	}
	if status, tr := exchange(pkceVerifier); status != 400 || tr.Error != "invalid_grant" {
		t.Fatalf("reused code: %d %+v", status, tr)
	}

	// Denying sends the user back with access_denied.
	denied := s.approve(params, ann.access, "deny")
	if denied.Query().Get("error") != "access_denied" || denied.Query().Get("code") != "" {
		t.Fatalf("deny: %s", denied)
	}
	// Approving needs a valid sign-in.
	form := url.Values{}
	for k, v := range params {
		form[k] = v
	}
	form.Set("decision", "approve")
	form.Set("id_token", "forged")
	res, _ := noRedirects.PostForm(s.srv.URL+"/oauth/authorize", form)
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forged ID token: status %d", res.StatusCode)
	}
}

func TestOAuthAuthorizePage(t *testing.T) {
	s := newTestServer(t)
	clientID := s.registerClient(claudeHook)
	get := func(params url.Values) *http.Response {
		res, err := noRedirects.Get(s.srv.URL + "/oauth/authorize?" + params.Encode())
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res := get(authorizeParams(clientID, claudeHook))
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	csp := res.Header.Get("Content-Security-Policy")
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "Connect Claude") || !strings.Contains(string(body), "claude.ai") ||
		!strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "form-action 'self' https://claude.ai") {
		t.Fatalf("page: %d, CSP %q", res.StatusCode, csp)
	}

	// An unknown client or an unregistered redirect gets an error page, never a redirect.
	for name, p := range map[string]url.Values{
		"unknown client":   authorizeParams("000000000000000000000000", claudeHook),
		"foreign redirect": authorizeParams(clientID, "https://evil.example/cb"),
	} {
		if res := get(p); res.StatusCode != http.StatusBadRequest || res.Header.Get("Location") != "" {
			t.Errorf("%s: status %d, Location %q", name, res.StatusCode, res.Header.Get("Location"))
		}
	}
	// Other problems go back to the client.
	noPKCE := authorizeParams(clientID, claudeHook)
	noPKCE.Del("code_challenge")
	if loc, _ := url.Parse(get(noPKCE).Header.Get("Location")); loc == nil || loc.Query().Get("error") != "invalid_request" {
		t.Fatalf("missing PKCE: %v", loc)
	}
	wrongResource := authorizeParams(clientID, claudeHook)
	wrongResource.Set("resource", "https://other.example/mcp")
	if loc, _ := url.Parse(get(wrongResource).Header.Get("Location")); loc == nil || loc.Query().Get("error") != "invalid_target" {
		t.Fatalf("other resource: %v", loc)
	}

	// Claude Code identifies itself with a metadata document and a loopback
	// redirect on whatever port it picked.
	cc := authorizeParams(codeClaude, "http://localhost:51234/callback")
	if res := get(cc); res.StatusCode != http.StatusOK {
		t.Fatalf("Claude Code: status %d", res.StatusCode)
	}
}

func TestOAuthTokensExpire(t *testing.T) {
	if oauthdomain.AccessTokenTTL != time.Hour || oauthdomain.CodeTTL > 10*time.Minute {
		t.Fatal("access tokens live an hour, codes at most 10 minutes (RFC 6749 section 4.1.2)")
	}
}
