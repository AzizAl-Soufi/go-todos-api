// Package storetest is the contract every identity persistence adapter must
// satisfy. The memory, MongoDB and PostgreSQL stores all run it, so the
// backends behave the same way the application layer expects.
package storetest

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
	"github.com/google/uuid"
)

// Stores is one backend's set of identity repositories, on empty storage.
type Stores struct {
	Users          application.UserRepository
	Sessions       application.SessionRepository
	Verifications  application.OneTimeTokenRepository
	PasswordResets application.OneTimeTokenRepository
	Passkeys       application.PasskeyRepository
	Challenges     application.PasskeyChallengeRepository
	Tx             tx.Runner
	// AtomicTx is true when Tx really rolls back (not tx.None).
	AtomicTx bool
	// MissingID is a well-formed ID for this backend that matches no record.
	MissingID string
}

// Run runs the contract; newStores must return fresh, empty storage.
func Run(t *testing.T, newStores func(t *testing.T) Stores) {
	t.Run("Users", func(t *testing.T) { testUsers(t, newStores(t)) })
	t.Run("Sessions", func(t *testing.T) { testSessions(t, newStores(t)) })
	t.Run("ConcurrentRevokeHasOneWinner", func(t *testing.T) { testConcurrentRevoke(t, newStores(t)) })
	t.Run("OneTimeTokens", func(t *testing.T) { testOneTimeTokens(t, newStores(t)) })
	t.Run("Passkeys", func(t *testing.T) { testPasskeys(t, newStores(t)) })
	t.Run("PasskeyChallenges", func(t *testing.T) { testPasskeyChallenges(t, newStores(t)) })
	t.Run("Transactions", func(t *testing.T) {
		s := newStores(t)
		if !s.AtomicTx {
			t.Skip("this backend runs without transactions")
		}
		testTransactions(t, s)
	})
}

var ctx = context.Background()

// now is truncated to what every backend stores (MongoDB keeps milliseconds).
func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func createUser(t *testing.T, s Stores, email string) *domain.User {
	t.Helper()
	u := &domain.User{Name: "Ann", Email: email, PasswordHash: "hash", CreatedAt: now()}
	if err := s.Users.Create(ctx, u); err != nil || u.ID == "" {
		t.Fatalf("create user: %v, id %q", err, u.ID)
	}
	return u
}

func mustFindUser(t *testing.T, s Stores, id string) *domain.User {
	t.Helper()
	u, err := s.Users.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	return u
}

func testUsers(t *testing.T, s Stores) {
	u := createUser(t, s, "ann@example.com")
	if err := s.Users.Create(ctx, &domain.User{Name: "B", Email: "ann@example.com", CreatedAt: now()}); !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("duplicate email: err = %v, want ErrEmailTaken", err)
	}

	got := mustFindUser(t, s, u.ID)
	if got.Name != "Ann" || got.Email != "ann@example.com" || got.PasswordHash != "hash" ||
		!got.CreatedAt.Equal(u.CreatedAt) || got.EmailVerified() || got.TokenVersion != 0 {
		t.Fatalf("round trip: got %+v, want %+v", got, u)
	}
	byEmail, err := s.Users.FindByEmail(ctx, "ann@example.com")
	if err != nil || byEmail.ID != u.ID {
		t.Fatalf("find by email: %+v, %v", byEmail, err)
	}

	first := now()
	for _, at := range []time.Time{first, first.Add(time.Hour)} {
		if err := s.Users.MarkEmailVerified(ctx, u.ID, at); err != nil {
			t.Fatalf("mark verified: %v", err)
		}
	}
	if got := mustFindUser(t, s, u.ID); got.EmailVerifiedAt == nil || !got.EmailVerifiedAt.Equal(first) {
		t.Fatalf("verified at = %v, want the first time %v", got.EmailVerifiedAt, first)
	}

	for range 2 {
		if err := s.Users.IncrementTokenVersion(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Users.SetPasswordHash(ctx, u.ID, "new-hash"); err != nil {
		t.Fatal(err)
	}
	if got := mustFindUser(t, s, u.ID); got.TokenVersion != 2 || got.PasswordHash != "new-hash" {
		t.Fatalf("token version %d, hash %q", got.TokenVersion, got.PasswordHash)
	}

	missing := s.MissingID
	notFound := map[string]error{
		"FindByID":              func() error { _, err := s.Users.FindByID(ctx, missing); return err }(),
		"FindByEmail":           func() error { _, err := s.Users.FindByEmail(ctx, "nobody@example.com"); return err }(),
		"MarkEmailVerified":     s.Users.MarkEmailVerified(ctx, missing, first),
		"IncrementTokenVersion": s.Users.IncrementTokenVersion(ctx, missing),
		"SetPasswordHash":       s.Users.SetPasswordHash(ctx, missing, "x"),
		"Delete":                s.Users.Delete(ctx, missing),
	}
	for name, err := range notFound {
		if !errors.Is(err, apperrors.ErrNotFound) {
			t.Errorf("%s(missing) = %v, want ErrNotFound", name, err)
		}
	}

	if err := s.Users.Delete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Users.FindByID(ctx, u.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleted user: err = %v", err)
	}
}

func newSession(t *testing.T, s Stores, userID, familyID, hash string) *domain.Session {
	t.Helper()
	at := now()
	sess := &domain.Session{UserID: userID, FamilyID: familyID, TokenHash: hash, ExpiresAt: at.Add(time.Hour), CreatedAt: at}
	if err := s.Sessions.Create(ctx, sess); err != nil || sess.ID == "" {
		t.Fatalf("create session: %v, id %q", err, sess.ID)
	}
	return sess
}

func revoked(t *testing.T, s Stores, hash string) bool {
	t.Helper()
	sess, err := s.Sessions.FindByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("find session %s: %v", hash, err)
	}
	return sess.Revoked()
}

func testSessions(t *testing.T, s Stores) {
	u1 := createUser(t, s, "one@example.com")
	u2 := createUser(t, s, "two@example.com")
	f1, f2, f3 := uuid.NewString(), uuid.NewString(), uuid.NewString()
	a := newSession(t, s, u1.ID, f1, "h1")
	newSession(t, s, u1.ID, f1, "h1b")
	newSession(t, s, u1.ID, f2, "h2")
	newSession(t, s, u2.ID, f3, "h3")

	got, err := s.Sessions.FindByTokenHash(ctx, "h1")
	if err != nil || got.ID != a.ID || got.UserID != u1.ID || got.FamilyID != f1 || !got.ExpiresAt.Equal(a.ExpiresAt) || got.Revoked() {
		t.Fatalf("round trip: got %+v (%v), want %+v", got, err, a)
	}
	if _, err := s.Sessions.FindByTokenHash(ctx, "nope"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("unknown hash: err = %v", err)
	}

	at := now()
	if ok, err := s.Sessions.Revoke(ctx, a.ID, at); !ok || err != nil {
		t.Fatalf("first revoke = %v, %v; want true", ok, err)
	}
	if ok, err := s.Sessions.Revoke(ctx, a.ID, at); ok || err != nil {
		t.Fatalf("second revoke = %v, %v; want false", ok, err)
	}
	if ok, err := s.Sessions.Revoke(ctx, s.MissingID, at); ok || err != nil {
		t.Fatalf("revoke missing = %v, %v; want false", ok, err)
	}

	if err := s.Sessions.RevokeFamily(ctx, f1, at); err != nil {
		t.Fatal(err)
	}
	if !revoked(t, s, "h1b") || revoked(t, s, "h2") {
		t.Fatal("RevokeFamily must revoke exactly that family")
	}
	if err := s.Sessions.RevokeAllForUser(ctx, u1.ID, at); err != nil {
		t.Fatal(err)
	}
	if !revoked(t, s, "h2") || revoked(t, s, "h3") {
		t.Fatal("RevokeAllForUser must revoke exactly that user's sessions")
	}

	if err := s.Sessions.DeleteAllForUser(ctx, u1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sessions.FindByTokenHash(ctx, "h1"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleted session still found: %v", err)
	}
	if revoked(t, s, "h3") {
		t.Fatal("DeleteAllForUser touched another user's session")
	}
}

// testConcurrentRevoke is what makes refresh-token rotation safe: when the
// same token is presented twice at once, only one request may rotate it.
func testConcurrentRevoke(t *testing.T, s Stores) {
	u := createUser(t, s, "race@example.com")
	sess := newSession(t, s, u.ID, uuid.NewString(), "race")

	const n = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.Sessions.Revoke(ctx, sess.ID, now())
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d concurrent revokes won, want exactly 1", wins)
	}
}

func token(userID, hash string) *domain.OneTimeToken {
	at := now()
	return &domain.OneTimeToken{UserID: userID, TokenHash: hash, ExpiresAt: at.Add(time.Hour), CreatedAt: at}
}

func testOneTimeTokens(t *testing.T, s Stores) {
	u1 := createUser(t, s, "one@example.com")
	u2 := createUser(t, s, "two@example.com")

	for name, repo := range map[string]application.OneTimeTokenRepository{"verifications": s.Verifications, "password resets": s.PasswordResets} {
		t.Run(name, func(t *testing.T) {
			old, fresh := name+"-old", name+"-new"
			for _, h := range []string{old, fresh} {
				if err := repo.Save(ctx, token(u1.ID, h)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := repo.Consume(ctx, old); !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatalf("saving again must replace the previous token: err = %v", err)
			}
			want := token(u1.ID, fresh)
			got, err := repo.Consume(ctx, fresh)
			if err != nil || got.UserID != u1.ID || got.TokenHash != fresh || !got.ExpiresAt.Truncate(time.Second).Equal(want.ExpiresAt.Truncate(time.Second)) {
				t.Fatalf("consume = %+v, %v", got, err)
			}
			if _, err := repo.Consume(ctx, fresh); !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatalf("a token must be single-use: err = %v", err)
			}

			gone := name + "-deleted"
			repo.Save(ctx, token(u2.ID, gone))
			if err := repo.DeleteForUser(ctx, u2.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.Consume(ctx, gone); !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatalf("DeleteForUser left the token: err = %v", err)
			}
			if err := repo.DeleteForUser(ctx, u2.ID); err != nil {
				t.Fatalf("deleting nothing must not fail: %v", err)
			}
		})
	}

	t.Run("kinds are independent", func(t *testing.T) {
		if err := s.Verifications.Save(ctx, token(u1.ID, "verify")); err != nil {
			t.Fatal(err)
		}
		if err := s.PasswordResets.Save(ctx, token(u1.ID, "reset")); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PasswordResets.Consume(ctx, "verify"); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatal("a verification token must not work as a password reset")
		}
		if _, err := s.PasswordResets.Consume(ctx, "reset"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Verifications.Consume(ctx, "verify"); err != nil {
			t.Fatal("consuming a reset must not touch the pending verification")
		}
	})
}

func testTransactions(t *testing.T, s Stores) {
	boom := errors.New("boom")
	var rolledBack string
	err := s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		u := &domain.User{Name: "Tx", Email: "rollback@example.com", CreatedAt: now()}
		if err := s.Users.Create(ctx, u); err != nil {
			return err
		}
		rolledBack = u.ID
		if err := s.Sessions.Create(ctx, &domain.Session{UserID: u.ID, FamilyID: uuid.NewString(), TokenHash: "tx", ExpiresAt: now().Add(time.Hour), CreatedAt: now()}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the function's error", err)
	}
	if _, err := s.Users.FindByID(ctx, rolledBack); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("user created in a failed transaction is visible: err = %v", err)
	}
	if _, err := s.Sessions.FindByTokenHash(ctx, "tx"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("session created in a failed transaction is visible: err = %v", err)
	}

	err = s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		u := &domain.User{Name: "Tx", Email: "commit@example.com", CreatedAt: now()}
		if err := s.Users.Create(ctx, u); err != nil {
			return err
		}
		// Nested calls join the outer transaction.
		return s.Tx.WithinTx(ctx, func(ctx context.Context) error { return s.Users.IncrementTokenVersion(ctx, u.ID) })
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Users.FindByEmail(ctx, "commit@example.com")
	if err != nil || u.TokenVersion != 1 {
		t.Fatalf("committed transaction: %+v, %v", u, err)
	}
}

func newPasskey(t *testing.T, s Stores, userID, name string, credentialID string, at time.Time) *domain.Passkey {
	t.Helper()
	p := &domain.Passkey{UserID: userID, Name: name, CredentialID: []byte(credentialID), Credential: []byte(`{"v":1}`), CreatedAt: at}
	if err := s.Passkeys.Create(ctx, p); err != nil || p.ID == "" {
		t.Fatalf("create passkey: %v, id %q", err, p.ID)
	}
	return p
}

func listPasskeys(t *testing.T, s Stores, userID string) []*domain.Passkey {
	t.Helper()
	ps, err := s.Passkeys.ListForUser(ctx, userID)
	if err != nil {
		t.Fatalf("list passkeys: %v", err)
	}
	return ps
}

func testPasskeys(t *testing.T, s Stores) {
	u1 := createUser(t, s, "one@example.com")
	u2 := createUser(t, s, "two@example.com")
	base := now()
	// Created out of order: the list is oldest first.
	second := newPasskey(t, s, u1.ID, "Phone", "cred-2", base.Add(time.Minute))
	first := newPasskey(t, s, u1.ID, "Laptop", "cred-1", base)
	other := newPasskey(t, s, u2.ID, "Key", "cred-3", base)

	dup := &domain.Passkey{UserID: u2.ID, Name: "Dup", CredentialID: []byte("cred-1"), Credential: []byte("{}"), CreatedAt: base}
	if err := s.Passkeys.Create(ctx, dup); !errors.Is(err, domain.ErrPasskeyExists) {
		t.Fatalf("duplicate credential id on another account: err = %v, want ErrPasskeyExists", err)
	}

	got := listPasskeys(t, s, u1.ID)
	if len(got) != 2 || got[0].ID != first.ID || got[1].ID != second.ID {
		t.Fatalf("list = %+v, want [%s %s]", got, first.ID, second.ID)
	}
	if p := got[0]; p.UserID != u1.ID || p.Name != "Laptop" || string(p.CredentialID) != "cred-1" ||
		string(p.Credential) != `{"v":1}` || !p.CreatedAt.Equal(base) || p.LastUsedAt != nil {
		t.Fatalf("round trip: %+v", p)
	}
	if ps := listPasskeys(t, s, s.MissingID); ps == nil || len(ps) != 0 {
		t.Fatalf("no passkeys must be an empty list, got %#v", ps)
	}

	used := now().Add(time.Hour)
	if err := s.Passkeys.RecordUse(ctx, first.ID, []byte(`{"v":2}`), used); err != nil {
		t.Fatal(err)
	}
	if p := listPasskeys(t, s, u1.ID)[0]; string(p.Credential) != `{"v":2}` || p.LastUsedAt == nil || !p.LastUsedAt.Equal(used) {
		t.Fatalf("after use: credential %s, last used %v", p.Credential, p.LastUsedAt)
	}

	notFound := map[string]error{
		"RecordUse(missing)":             s.Passkeys.RecordUse(ctx, s.MissingID, []byte("{}"), used),
		"Delete(missing)":                s.Passkeys.Delete(ctx, u1.ID, s.MissingID),
		"Delete(another user's passkey)": s.Passkeys.Delete(ctx, u1.ID, other.ID),
	}
	for name, err := range notFound {
		if !errors.Is(err, apperrors.ErrNotFound) {
			t.Errorf("%s = %v, want ErrNotFound", name, err)
		}
	}

	if err := s.Passkeys.Delete(ctx, u1.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if got := listPasskeys(t, s, u1.ID); len(got) != 1 || got[0].ID != second.ID {
		t.Fatalf("after delete: %+v", got)
	}
	// A deleted credential ID can be registered again.
	newPasskey(t, s, u2.ID, "Again", "cred-1", base)

	if err := s.Passkeys.DeleteAllForUser(ctx, u1.ID); err != nil {
		t.Fatal(err)
	}
	if len(listPasskeys(t, s, u1.ID)) != 0 || len(listPasskeys(t, s, u2.ID)) != 2 {
		t.Fatal("DeleteAllForUser must delete exactly that user's passkeys")
	}
}

func challenge(userID, hash string, createdAt time.Time, ttl time.Duration) *domain.PasskeyChallenge {
	return &domain.PasskeyChallenge{TokenHash: hash, UserID: userID, State: []byte("state-" + hash), ExpiresAt: createdAt.Add(ttl), CreatedAt: createdAt}
}

func testPasskeyChallenges(t *testing.T, s Stores) {
	u := createUser(t, s, "one@example.com")
	at := now()

	for _, c := range []*domain.PasskeyChallenge{challenge(u.ID, "reg", at, time.Minute), challenge("", "login", at, time.Minute)} {
		if err := s.Challenges.Save(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Challenges.Consume(ctx, "reg")
	if err != nil || got.UserID != u.ID || !bytes.Equal(got.State, []byte("state-reg")) ||
		!got.ExpiresAt.Equal(at.Add(time.Minute)) || !got.CreatedAt.Equal(at) {
		t.Fatalf("consume registration challenge = %+v, %v", got, err)
	}
	if _, err := s.Challenges.Consume(ctx, "reg"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("a challenge must be single-use: err = %v", err)
	}
	if got, err := s.Challenges.Consume(ctx, "login"); err != nil || got.UserID != "" {
		t.Fatalf("consume login challenge = %+v, %v", got, err)
	}

	// Saving prunes challenges that expired before the new one was created,
	// and keeps the rest.
	if err := s.Challenges.Save(ctx, challenge("", "stale", at.Add(-time.Hour), time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.Challenges.Save(ctx, challenge("", "live", at.Add(-time.Hour), 2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Challenges.Save(ctx, challenge("", "new", at, time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Challenges.Consume(ctx, "stale"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("expired challenge not pruned: err = %v", err)
	}
	for _, h := range []string{"live", "new"} {
		if _, err := s.Challenges.Consume(ctx, h); err != nil {
			t.Fatalf("challenge %s pruned too early: %v", h, err)
		}
	}

	s.Challenges.Save(ctx, challenge(u.ID, "mine", at, time.Minute))
	s.Challenges.Save(ctx, challenge("", "anon", at, time.Minute))
	if err := s.Challenges.DeleteForUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Challenges.Consume(ctx, "mine"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("DeleteForUser left the challenge: err = %v", err)
	}
	if _, err := s.Challenges.Consume(ctx, "anon"); err != nil {
		t.Fatalf("DeleteForUser removed a sign-in challenge: %v", err)
	}
}
