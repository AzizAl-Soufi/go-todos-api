// Package storetest is the contract every oauth store must satisfy; the
// memory, MongoDB and PostgreSQL stores all run it.
package storetest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// Stores are one backend's oauth stores on empty storage, plus well-formed
// IDs for that backend.
type Stores struct {
	Clients application.ClientRepository
	Codes   application.CodeRepository
	Grants  application.GrantRepository
	NewID   func() string
}

func Run(t *testing.T, newStores func(t *testing.T) Stores) {
	t.Run("Clients", func(t *testing.T) { testClients(t, newStores(t)) })
	t.Run("Codes", func(t *testing.T) { testCodes(t, newStores(t)) })
	t.Run("Grants", func(t *testing.T) { testGrants(t, newStores(t)) })
	t.Run("Rotation", func(t *testing.T) { testRotation(t, newStores(t)) })
	t.Run("DeleteExpiredCodes", func(t *testing.T) { testDeleteExpiredCodes(t, newStores(t)) })
	t.Run("DeleteExpiredGrants", func(t *testing.T) { testDeleteExpiredGrants(t, newStores(t)) })
	t.Run("DeleteUnusedClients", func(t *testing.T) { testDeleteUnusedClients(t, newStores(t)) })
}

var ctx = context.Background()

// now is truncated to what every backend stores (MongoDB keeps milliseconds).
func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func expectNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("%s: err = %v, want ErrNotFound", what, err)
	}
}

func testClients(t *testing.T, s Stores) {
	c := &domain.Client{
		Name:         "Claude",
		RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback", "http://localhost/callback"},
		Kind:         domain.Registered,
		CreatedAt:    now(),
	}
	if err := s.Clients.Create(ctx, c); err != nil || c.ID == "" {
		t.Fatalf("create: %v, id %q", err, c.ID)
	}
	got, err := s.Clients.Get(ctx, c.ID)
	if err != nil || got.Name != "Claude" || !slices.Equal(got.RedirectURIs, c.RedirectURIs) ||
		got.Kind != domain.Registered || !got.CreatedAt.Equal(c.CreatedAt) {
		t.Fatalf("round trip: %+v (%v)", got, err)
	}
	_, err = s.Clients.Get(ctx, s.NewID())
	expectNotFound(t, "unknown client", err)
	if _, err := s.Clients.Get(ctx, "not-an-id"); !errors.Is(err, apperrors.ErrInvalidID) {
		t.Fatalf("malformed ID: err = %v", err)
	}
}

func newCode(s Stores, hash, userID string, at time.Time) *domain.Code {
	return &domain.Code{
		Hash: hash, ClientID: "https://claude.ai/oauth/claude-code-client-metadata", ClientName: "Claude Code",
		RedirectURI: "http://localhost:3118/callback", Challenge: "challenge", Resource: "https://api.example.com/mcp",
		Scope: domain.Scope, Owner: domain.Owner{UserID: userID, Name: "Ann", Email: "ann@example.com", EmailVerified: true},
		ExpiresAt: at.Add(domain.CodeTTL), CreatedAt: at,
	}
}

func testCodes(t *testing.T, s Stores) {
	ann, bo := s.NewID(), s.NewID()
	at := now()
	stale := newCode(s, "hash-stale", ann, at.Add(-time.Hour))
	if err := s.Codes.Save(ctx, stale); err != nil {
		t.Fatal(err)
	}
	code := newCode(s, "hash-1", ann, at)
	if err := s.Codes.Save(ctx, code); err != nil {
		t.Fatal(err)
	}
	_, err := s.Codes.Consume(ctx, "hash-stale")
	expectNotFound(t, "saving a code deletes expired ones", err)

	got, err := s.Codes.Consume(ctx, "hash-1")
	if err != nil || got.ClientID != code.ClientID || got.RedirectURI != code.RedirectURI || got.Owner != code.Owner ||
		got.Challenge != "challenge" || got.Resource != code.Resource || !got.ExpiresAt.Equal(code.ExpiresAt) {
		t.Fatalf("round trip: %+v (%v)", got, err)
	}
	_, err = s.Codes.Consume(ctx, "hash-1")
	expectNotFound(t, "a code is single use", err)

	for hash, user := range map[string]string{"hash-ann": ann, "hash-bo": bo} {
		if err := s.Codes.Save(ctx, newCode(s, hash, user, at)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Codes.DeleteForUser(ctx, ann); err != nil {
		t.Fatal(err)
	}
	_, err = s.Codes.Consume(ctx, "hash-ann")
	expectNotFound(t, "DeleteForUser", err)
	if _, err := s.Codes.Consume(ctx, "hash-bo"); err != nil {
		t.Fatalf("DeleteForUser removed another user's code: %v", err)
	}
}

func newGrant(t *testing.T, s Stores, userID, client string, at time.Time) *domain.Grant {
	t.Helper()
	g := &domain.Grant{
		ClientID: client, ClientName: client + " app", Scope: domain.Scope, Resource: "https://api.example.com/mcp",
		Owner:      domain.Owner{UserID: userID, Name: "Ann", Email: "ann@example.com", EmailVerified: true},
		AccessHash: "access-" + client, AccessExpiresAt: at.Add(domain.AccessTokenTTL),
		RefreshHash: "refresh-" + client, RefreshExpiresAt: at.Add(domain.RefreshTokenTTL),
		CreatedAt: at, LastUsedAt: at,
	}
	if err := s.Grants.Create(ctx, g); err != nil || g.ID == "" {
		t.Fatalf("create grant: %v, id %q", err, g.ID)
	}
	return g
}

func grantIDs(list []*domain.Grant) []string {
	out := []string{}
	for _, g := range list {
		out = append(out, g.ID)
	}
	return out
}

func testGrants(t *testing.T, s Stores) {
	ann, bo := s.NewID(), s.NewID()
	at := now()
	first := newGrant(t, s, ann, "first", at)
	second := newGrant(t, s, ann, "second", at.Add(time.Second))
	theirs := newGrant(t, s, bo, "theirs", at)

	got, err := s.Grants.Get(ctx, first.ID)
	if err != nil || got.ClientID != "first" || got.ClientName != "first app" || got.Owner != first.Owner ||
		got.AccessHash != "access-first" || got.RefreshHash != "refresh-first" || got.Scope != domain.Scope ||
		got.Resource != first.Resource || !got.AccessExpiresAt.Equal(first.AccessExpiresAt) ||
		!got.RefreshExpiresAt.Equal(first.RefreshExpiresAt) || !got.LastUsedAt.Equal(at) {
		t.Fatalf("round trip: %+v (%v)", got, err)
	}
	list, err := s.Grants.ListForUser(ctx, ann)
	if err != nil || !slices.Equal(grantIDs(list), []string{second.ID, first.ID}) {
		t.Fatalf("ListForUser = %v (%v), want most recently used first", grantIDs(list), err)
	}

	expectNotFound(t, "Delete another user's grant", s.Grants.Delete(ctx, ann, theirs.ID))
	expectNotFound(t, "Delete missing", s.Grants.Delete(ctx, ann, s.NewID()))
	if err := s.Grants.Delete(ctx, ann, first.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.Grants.Get(ctx, first.ID)
	expectNotFound(t, "deleted grant", err)

	if err := s.Grants.Revoke(ctx, theirs.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{theirs.ID, s.NewID(), "not-an-id"} {
		if err := s.Grants.Revoke(ctx, id); err != nil {
			t.Fatalf("revoking a missing grant %q: %v", id, err)
		}
	}

	if err := s.Grants.DeleteForUser(ctx, ann); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Grants.ListForUser(ctx, ann); len(list) != 0 {
		t.Fatalf("DeleteForUser left %v", grantIDs(list))
	}

	// Creating a grant clears the user's grants that can't be refreshed.
	expired := newGrant(t, s, bo, "expired", at.Add(-2*domain.RefreshTokenTTL))
	newGrant(t, s, bo, "fresh", at)
	_, err = s.Grants.Get(ctx, expired.ID)
	expectNotFound(t, "grant with an expired refresh token", err)
}

func testRotation(t *testing.T, s Stores) {
	at := now()
	g := newGrant(t, s, s.NewID(), "client", at)
	later := at.Add(time.Hour)
	r := application.Rotation{
		GrantID: g.ID, RefreshHash: "refresh-client",
		AccessHash: "access-2", AccessExpiresAt: later.Add(domain.AccessTokenTTL),
		NewRefreshHash: "refresh-2", RefreshExpiresAt: later.Add(domain.RefreshTokenTTL), UsedAt: later,
	}
	if err := s.Grants.Rotate(ctx, r); err != nil {
		t.Fatal(err)
	}
	got, err := s.Grants.Get(ctx, g.ID)
	if err != nil || got.AccessHash != "access-2" || got.RefreshHash != "refresh-2" ||
		!got.AccessExpiresAt.Equal(r.AccessExpiresAt) || !got.RefreshExpiresAt.Equal(r.RefreshExpiresAt) ||
		!got.LastUsedAt.Equal(later) || !got.CreatedAt.Equal(at) {
		t.Fatalf("after rotation: %+v (%v)", got, err)
	}
	// The same refresh token again: it is no longer current.
	expectNotFound(t, "rotating with a used refresh token", s.Grants.Rotate(ctx, r))
	r.GrantID = s.NewID()
	expectNotFound(t, "rotating a missing grant", s.Grants.Rotate(ctx, r))
}

func expectDeleted(t *testing.T, what string, n int64, err error, want int64) {
	t.Helper()
	if err != nil || n != want {
		t.Fatalf("%s: deleted %d (%v), want %d", what, n, err, want)
	}
}

func testDeleteExpiredCodes(t *testing.T, s Stores) {
	at := now()
	// Saved in this order because saving a code deletes the codes that
	// expired before it was created.
	for _, c := range []*domain.Code{
		newCode(s, "fresh", s.NewID(), at),
		newCode(s, "expires-now", s.NewID(), at.Add(-domain.CodeTTL)),
		newCode(s, "stale-1", s.NewID(), at.Add(-time.Hour)),
		newCode(s, "stale-2", s.NewID(), at.Add(-time.Hour)),
	} {
		if err := s.Codes.Save(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.Codes.DeleteExpired(ctx, at, 2)
	expectDeleted(t, "first batch", n, err, 2)
	n, err = s.Codes.DeleteExpired(ctx, at, 2)
	expectDeleted(t, "second batch", n, err, 1)
	n, err = s.Codes.DeleteExpired(ctx, at, 2)
	expectDeleted(t, "nothing left", n, err, 0)
	for _, hash := range []string{"expires-now", "stale-1", "stale-2"} {
		_, err := s.Codes.Consume(ctx, hash)
		expectNotFound(t, "expired code "+hash, err)
	}
	if _, err := s.Codes.Consume(ctx, "fresh"); err != nil {
		t.Fatalf("DeleteExpired removed a code that hasn't expired: %v", err)
	}
}

func testDeleteExpiredGrants(t *testing.T, s Stores) {
	at := now()
	// Different users: creating a grant clears its own user's expired ones.
	fresh := newGrant(t, s, s.NewID(), "fresh", at.Add(-time.Hour))
	expiresNow := newGrant(t, s, s.NewID(), "expires-now", at.Add(-domain.RefreshTokenTTL))
	stale := newGrant(t, s, s.NewID(), "stale", at.Add(-2*domain.RefreshTokenTTL))

	n, err := s.Grants.DeleteExpired(ctx, at, 1)
	expectDeleted(t, "first batch", n, err, 1)
	n, err = s.Grants.DeleteExpired(ctx, at, 10)
	expectDeleted(t, "second batch", n, err, 1)
	n, err = s.Grants.DeleteExpired(ctx, at, 10)
	expectDeleted(t, "nothing left", n, err, 0)
	for _, g := range []*domain.Grant{expiresNow, stale} {
		_, err := s.Grants.Get(ctx, g.ID)
		expectNotFound(t, "grant with an expired refresh token "+g.ClientID, err)
	}
	if _, err := s.Grants.Get(ctx, fresh.ID); err != nil {
		t.Fatalf("DeleteExpired removed a grant that can be refreshed: %v", err)
	}
}

func testDeleteUnusedClients(t *testing.T, s Stores) {
	at := now()
	cutoff := at.Add(-domain.UnusedClientTTL)
	newClient := func(created time.Time) *domain.Client {
		t.Helper()
		c := &domain.Client{Name: "Claude", RedirectURIs: []string{"https://claude.ai/cb"}, Kind: domain.Registered, CreatedAt: created}
		if err := s.Clients.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	old := cutoff.Add(-time.Hour)
	unused1, unused2 := newClient(old.Add(-time.Minute)), newClient(old)
	withGrant, withCode := newClient(old), newClient(old)
	recent := newClient(cutoff.Add(time.Hour))

	grant := newGrant(t, s, s.NewID(), withGrant.ID, at)
	code := newCode(s, "code", s.NewID(), at)
	code.ClientID = withCode.ID
	if err := s.Codes.Save(ctx, code); err != nil {
		t.Fatal(err)
	}
	// A metadata document client's grant names a URL, not a stored client.
	newGrant(t, s, s.NewID(), "https://claude.ai/oauth/claude-code-client-metadata", at)

	n, err := s.Clients.DeleteUnused(ctx, cutoff, 1)
	expectDeleted(t, "first batch", n, err, 1)
	_, err = s.Clients.Get(ctx, unused1.ID)
	expectNotFound(t, "the oldest unused client goes first", err)
	n, err = s.Clients.DeleteUnused(ctx, cutoff, 10)
	expectDeleted(t, "second batch", n, err, 1)
	n, err = s.Clients.DeleteUnused(ctx, cutoff, 10)
	expectDeleted(t, "nothing left", n, err, 0)

	_, err = s.Clients.Get(ctx, unused2.ID)
	expectNotFound(t, "old unused client", err)
	for what, c := range map[string]*domain.Client{
		"a client a grant references":    withGrant,
		"a client a code references":     withCode,
		"a client newer than the cutoff": recent,
	} {
		if _, err := s.Clients.Get(ctx, c.ID); err != nil {
			t.Fatalf("DeleteUnused removed %s: %v", what, err)
		}
	}

	// Once its grant is gone, the client is unused.
	if err := s.Grants.Revoke(ctx, grant.ID); err != nil {
		t.Fatal(err)
	}
	n, err = s.Clients.DeleteUnused(ctx, cutoff, 10)
	expectDeleted(t, "after its grant was deleted", n, err, 1)
}
