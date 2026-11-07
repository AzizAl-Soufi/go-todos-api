// Package storetest is the contract every identity persistence adapter must
// satisfy. The memory, MongoDB and PostgreSQL stores all run it, so the
// backends behave the same way the application layer expects.
package storetest

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

// Stores is one backend's set of identity repositories, on empty storage.
type Stores struct {
	Users      application.UserRepository
	Passkeys   application.PasskeyRepository
	Challenges application.PasskeyChallengeRepository
	Tx         tx.Runner
	// AtomicTx is true when Tx really rolls back (not tx.None).
	AtomicTx bool
	// MissingID is a well-formed ID for this backend that matches no record.
	MissingID string
}

// Run runs the contract; newStores must return fresh, empty storage.
func Run(t *testing.T, newStores func(t *testing.T) Stores) {
	t.Run("Users", func(t *testing.T) { testUsers(t, newStores(t)) })
	t.Run("FirebaseLinks", func(t *testing.T) { testFirebaseLinks(t, newStores(t)) })
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
	u := &domain.User{Name: "Ann", Email: email, FirebaseUID: "fb-" + email, CreatedAt: now()}
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
	if got.Name != "Ann" || got.Email != "ann@example.com" || got.FirebaseUID != "fb-ann@example.com" ||
		!got.CreatedAt.Equal(u.CreatedAt) || got.EmailVerified() || got.SessionsValidAfter != nil {
		t.Fatalf("round trip: got %+v, want %+v", got, u)
	}
	byEmail, err := s.Users.FindByEmail(ctx, "ann@example.com")
	if err != nil || byEmail.ID != u.ID {
		t.Fatalf("find by email: %+v, %v", byEmail, err)
	}
	byUID, err := s.Users.FindByFirebaseUID(ctx, "fb-ann@example.com")
	if err != nil || byUID.ID != u.ID {
		t.Fatalf("find by firebase uid: %+v, %v", byUID, err)
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

	for _, at := range []time.Time{first, first.Add(time.Hour)} {
		if err := s.Users.RevokeSessions(ctx, u.ID, at); err != nil {
			t.Fatal(err)
		}
	}
	if got := mustFindUser(t, s, u.ID); got.SessionsValidAfter == nil || !got.SessionsValidAfter.Equal(first.Add(time.Hour)) {
		t.Fatalf("sessions valid after = %v, want the latest revocation", got.SessionsValidAfter)
	}

	missing := s.MissingID
	notFound := map[string]error{
		"FindByID":          func() error { _, err := s.Users.FindByID(ctx, missing); return err }(),
		"FindByEmail":       func() error { _, err := s.Users.FindByEmail(ctx, "nobody@example.com"); return err }(),
		"FindByFirebaseUID": func() error { _, err := s.Users.FindByFirebaseUID(ctx, "nobody"); return err }(),
		"LinkFirebaseUID":   s.Users.LinkFirebaseUID(ctx, missing, "x"),
		"MarkEmailVerified": s.Users.MarkEmailVerified(ctx, missing, first),
		"RevokeSessions":    s.Users.RevokeSessions(ctx, missing, first),
		"Delete":            s.Users.Delete(ctx, missing),
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

// testFirebaseLinks covers accounts from before Firebase: any number may be
// unlinked, each is linked once, and a Firebase user links one account.
func testFirebaseLinks(t *testing.T, s Stores) {
	legacy := func(email string) *domain.User {
		u := &domain.User{Name: "Old", Email: email, CreatedAt: now()}
		if err := s.Users.Create(ctx, u); err != nil {
			t.Fatalf("create unlinked user %s: %v", email, err)
		}
		return u
	}
	a, b := legacy("a@example.com"), legacy("b@example.com")
	if _, err := s.Users.FindByFirebaseUID(ctx, ""); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("an empty UID must match no account: err = %v", err)
	}

	if err := s.Users.LinkFirebaseUID(ctx, a.ID, "uid-a"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Users.FindByFirebaseUID(ctx, "uid-a"); err != nil || got.ID != a.ID {
		t.Fatalf("linked account: %+v, %v", got, err)
	}
	if err := s.Users.LinkFirebaseUID(ctx, a.ID, "uid-other"); !errors.Is(err, domain.ErrFirebaseUIDTaken) {
		t.Fatalf("relinking an account: err = %v, want ErrFirebaseUIDTaken", err)
	}
	if err := s.Users.LinkFirebaseUID(ctx, b.ID, "uid-a"); !errors.Is(err, domain.ErrFirebaseUIDTaken) {
		t.Fatalf("one Firebase user on two accounts: err = %v, want ErrFirebaseUIDTaken", err)
	}
	dup := &domain.User{Name: "Dup", Email: "c@example.com", FirebaseUID: "uid-a", CreatedAt: now()}
	if err := s.Users.Create(ctx, dup); !errors.Is(err, domain.ErrFirebaseUIDTaken) {
		t.Fatalf("creating with a taken Firebase UID: err = %v, want ErrFirebaseUIDTaken", err)
	}
	if got := mustFindUser(t, s, b.ID); got.FirebaseUID != "" {
		t.Fatalf("failed link changed the account: %+v", got)
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

func testTransactions(t *testing.T, s Stores) {
	boom := errors.New("boom")
	var rolledBack string
	err := s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		u := &domain.User{Name: "Tx", Email: "rollback@example.com", CreatedAt: now()}
		if err := s.Users.Create(ctx, u); err != nil {
			return err
		}
		rolledBack = u.ID
		p := &domain.Passkey{UserID: u.ID, Name: "Tx", CredentialID: []byte("tx"), Credential: []byte("{}"), CreatedAt: now()}
		if err := s.Passkeys.Create(ctx, p); err != nil {
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
	if ps := listPasskeys(t, s, rolledBack); len(ps) != 0 {
		t.Fatalf("passkey created in a failed transaction is visible: %+v", ps)
	}

	err = s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		u := &domain.User{Name: "Tx", Email: "commit@example.com", CreatedAt: now()}
		if err := s.Users.Create(ctx, u); err != nil {
			return err
		}
		// Nested calls join the outer transaction.
		return s.Tx.WithinTx(ctx, func(ctx context.Context) error { return s.Users.LinkFirebaseUID(ctx, u.ID, "uid-commit") })
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := s.Users.FindByEmail(ctx, "commit@example.com")
	if err != nil || u.FirebaseUID != "uid-commit" {
		t.Fatalf("committed transaction: %+v, %v", u, err)
	}
}
