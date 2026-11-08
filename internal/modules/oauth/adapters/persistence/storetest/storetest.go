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
