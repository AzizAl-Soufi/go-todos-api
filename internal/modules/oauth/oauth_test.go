package oauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"slices"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/opaque"
)

type accounts struct{ userID string }

func (a accounts) Authenticate(context.Context, string) (auth.Principal, error) {
	return auth.Principal{UserID: a.userID, Email: "ann@example.com"}, nil
}

const verifier = "dBjftJeZ4CVP-mJ92K9cX5tRjE6g1nq3ypMPyQKmqZw"

func TestConnectedAppsAreCapped(t *testing.T) {
	ctx := context.Background()
	user := bson.NewObjectID().Hex()
	grants := memstore.NewGrants()
	m := oauth.New(oauth.Deps{
		Clients: memstore.NewClients(), Codes: memstore.NewCodes(), Grants: grants,
		Accounts: accounts{userID: user},
		Config:   oauth.Config{Issuer: "https://api.example.com", Resource: "https://api.example.com/mcp"},
	})
	clientID, err := m.Commands.RegisterClient.Handle(ctx, command.RegisterClient{Name: "Claude", RedirectURIs: []string{"https://claude.ai/cb"}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(verifier))
	req := domain.AuthorizationRequest{
		ResponseType: "code", ClientID: clientID, RedirectURI: "https://claude.ai/cb",
		CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), CodeChallengeMethod: "S256",
	}

	var first command.TokenSet
	for i := range domain.MaxGrantsPerUser + 1 {
		code, err := m.Commands.Approve.Handle(ctx, command.Approve{Request: req, IDToken: "id-token"})
		if err != nil {
			t.Fatal(err)
		}
		tokens, err := m.Commands.ExchangeCode.Handle(ctx, command.ExchangeCode{
			ClientID: clientID, Code: code, RedirectURI: req.RedirectURI, CodeVerifier: verifier,
		})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = tokens
		}
	}
	list, _ := grants.ListForUser(ctx, user)
	if len(list) != domain.MaxGrantsPerUser {
		t.Fatalf("%d grants, want at most %d", len(list), domain.MaxGrantsPerUser)
	}
	if _, err := m.AuthenticateToken(ctx, first.AccessToken); err != oauth.ErrInvalidToken {
		t.Fatalf("the least recently used grant must be revoked: err = %v", err)
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestCleanup(t *testing.T) {
	ctx := context.Background()
	user := bson.NewObjectID().Hex()
	stores := memstore.New()
	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	m := oauth.New(oauth.Deps{
		Clients: stores.Clients, Codes: stores.Codes, Grants: stores.Grants,
		Accounts: accounts{userID: user},
		Config:   oauth.Config{Issuer: "https://api.example.com", Resource: "https://api.example.com/mcp"},
		Now:      clk.now,
	})
	register := func() string {
		t.Helper()
		id, err := m.Commands.RegisterClient.Handle(ctx, command.RegisterClient{Name: "Claude", RedirectURIs: []string{"https://claude.ai/cb"}})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	sum := sha256.Sum256([]byte(verifier))
	approve := func(clientID string) string {
		t.Helper()
		code, err := m.Commands.Approve.Handle(ctx, command.Approve{IDToken: "id-token", Request: domain.AuthorizationRequest{
			ResponseType: "code", ClientID: clientID, RedirectURI: "https://claude.ai/cb",
			CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), CodeChallengeMethod: "S256",
		}})
		if err != nil {
			t.Fatal(err)
		}
		return code
	}
	cleanup := func(cmd command.Cleanup) {
		t.Helper()
		if err := m.Commands.Cleanup.Handle(ctx, cmd); err != nil {
			t.Fatal(err)
		}
	}
	exists := func(clientID string) bool {
		t.Helper()
		_, err := stores.Clients.Get(ctx, clientID)
		if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatal(err)
		}
		return err == nil
	}

	// connected keeps a refreshed grant; abandoned's grant expires;
	// pending only ever got a code; idle never got anything.
	connected, abandoned, pending, idle := register(), register(), register(), register()
	exchange := func(clientID string) command.TokenSet {
		t.Helper()
		tokens, err := m.Commands.ExchangeCode.Handle(ctx, command.ExchangeCode{
			ClientID: clientID, Code: approve(clientID), RedirectURI: "https://claude.ai/cb", CodeVerifier: verifier,
		})
		if err != nil {
			t.Fatal(err)
		}
		return tokens
	}
	tokens := exchange(connected)
	abandonedTokens := exchange(abandoned)
	pendingCode := approve(pending)

	// Within the TTLs nothing goes, though the code has expired.
	clk.advance(domain.CodeTTL)
	cleanup(command.Cleanup{})
	if _, err := stores.Codes.Consume(ctx, opaque.Hash(pendingCode)); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("expired code: err = %v", err)
	}
	for _, id := range []string{connected, abandoned, pending, idle} {
		if !exists(id) {
			t.Fatalf("client %s deleted before it was %s old", id, domain.UnusedClientTTL)
		}
	}

	clk.advance(29 * 24 * time.Hour)
	if _, err := m.Commands.Refresh.Handle(ctx, command.Refresh{ClientID: connected, RefreshToken: tokens.RefreshToken}); err != nil {
		t.Fatal(err)
	}
	clk.advance(2 * 24 * time.Hour)
	cleanup(command.Cleanup{BatchSize: 1}) // several batches

	if exists(abandoned) || exists(pending) || exists(idle) {
		t.Fatal("unused clients older than the TTL must be deleted")
	}
	if !exists(connected) {
		t.Fatal("a client with a live grant must be kept")
	}
	list, _ := stores.Grants.ListForUser(ctx, user)
	if len(list) != 1 || list[0].ClientID != connected {
		t.Fatalf("grants after cleanup: %+v, want only the refreshed one", list)
	}
	if _, err := m.AuthenticateToken(ctx, abandonedTokens.AccessToken); !errors.Is(err, oauth.ErrInvalidToken) {
		t.Fatalf("expired grant still authenticates: %v", err)
	}

	// MaxPerRun bounds a run; the next one carries on. Running it again
	// with nothing to do is harmless. connected's grant expires too, which
	// makes four unused clients.
	more := []string{connected, register(), register(), register()}
	clk.advance(domain.UnusedClientTTL + time.Hour)
	cleanup(command.Cleanup{BatchSize: 1, MaxPerRun: 2})
	left := 0
	for _, id := range more {
		if exists(id) {
			left++
		}
	}
	if left != 2 {
		t.Fatalf("%d clients left after a run capped at 2, want 2", left)
	}
	cleanup(command.Cleanup{})
	cleanup(command.Cleanup{})
	for _, id := range more {
		if exists(id) {
			t.Fatalf("client %s left after the next run", id)
		}
	}
}

func TestCleanupJob(t *testing.T) {
	m := oauth.New(oauth.Deps{Clients: memstore.NewClients(), Codes: memstore.NewCodes(), Grants: memstore.NewGrants()})
	r := jobs.NewRegistry()
	m.RegisterJobs(r)
	if !slices.Contains(r.Kinds(), oauth.JobCleanup) {
		t.Fatalf("registered kinds %v, want %s", r.Kinds(), oauth.JobCleanup)
	}
	want := []jobs.Schedule{{Kind: oauth.JobCleanup, Every: time.Hour}}
	if got := m.Schedules(); !slices.Equal(got, want) {
		t.Fatalf("Schedules() = %+v, want %+v", got, want)
	}
}
