package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	valid := map[string]string{
		"a@b.co":               "a@b.co",
		"  Ann@Example.COM ":   "ann@example.com", //nolint:gocritic // mapKey: the padding is the input under test
		"first.last+tag@x.org": "first.last+tag@x.org",
	}
	for in, want := range valid {
		if got, err := NormalizeEmail(in); err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	for _, in := range []string{"", "   ", "nope", "Ann <a@b.co>", "a@", strings.Repeat("a", 250) + "@b.co"} {
		if _, err := NormalizeEmail(in); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("NormalizeEmail(%q) err = %v, want ErrInvalidEmail", in, err)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	if got, err := NormalizeName("  Ann  "); err != nil || got != "Ann" {
		t.Fatalf("NormalizeName = %q, %v", got, err)
	}
	if _, err := NormalizeName(" "); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("blank: err = %v", err)
	}
	if _, err := NormalizeName(strings.Repeat("é", MaxNameLength+1)); !errors.Is(err, ErrNameTooLong) {
		t.Fatalf("too long: err = %v", err)
	}
	if _, err := NormalizeName(strings.Repeat("é", MaxNameLength)); err != nil {
		t.Fatalf("max length counts characters, not bytes: %v", err)
	}
}

func TestNewUser(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("x", 3600))
	u, err := NewUser("uid-1", " Ann ", "ANN@example.com", false, now)
	if err != nil {
		t.Fatal(err)
	}
	if u.FirebaseUID != "uid-1" || u.Name != "Ann" || u.Email != "ann@example.com" || u.CreatedAt.Location() != time.UTC || u.EmailVerified() {
		t.Fatalf("unexpected user: %+v", u)
	}
	if u, _ := NewUser("uid-2", " ", "bo.smith@example.com", true, now); u.Name != "bo.smith" || !u.EmailVerified() {
		t.Fatalf("name falls back to the email's local part, verified as given: %+v", u)
	}
	if _, err := NewUser("", "Ann", "ann@example.com", true, now); !errors.Is(err, ErrInvalidIDToken) {
		t.Fatalf("no Firebase UID: err = %v", err)
	}
	if _, err := NewUser("uid", "Ann", "nope", true, now); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("bad email: err = %v", err)
	}
}

func TestSignInRevoked(t *testing.T) {
	cutoff := time.Date(2026, 1, 1, 10, 0, 0, 500_000_000, time.UTC)
	u := User{}
	if u.SignInRevoked(cutoff.Add(-time.Hour)) {
		t.Fatal("nothing is revoked before logging out everywhere")
	}
	u.SessionsValidAfter = &cutoff
	if !u.SignInRevoked(cutoff.Add(-time.Second)) {
		t.Fatal("an earlier sign-in must be revoked")
	}
	// auth_time has whole seconds: a sign-in in the same second still counts.
	if u.SignInRevoked(cutoff.Truncate(time.Second)) || u.SignInRevoked(cutoff.Add(time.Second)) {
		t.Fatal("a sign-in in or after the cutoff second must stay valid")
	}
}

func TestNewPasskey(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("x", 3600))
	p, err := NewPasskey("u1", "  ", []byte("id"), []byte("rec"), now)
	if err != nil || p.Name != DefaultPasskeyName || p.CreatedAt.Location() != time.UTC || p.LastUsedAt != nil {
		t.Fatalf("NewPasskey = %+v, %v", p, err)
	}
	if got, err := NormalizePasskeyName(" Laptop "); err != nil || got != "Laptop" {
		t.Fatalf("NormalizePasskeyName = %q, %v", got, err)
	}
	if _, err := NormalizePasskeyName(strings.Repeat("é", MaxPasskeyNameLength)); err != nil {
		t.Fatalf("max length counts characters: %v", err)
	}
	if _, err := NewPasskey("u1", strings.Repeat("x", MaxPasskeyNameLength+1), nil, nil, now); !errors.Is(err, ErrPasskeyNameTooLong) {
		t.Fatalf("too long: err = %v", err)
	}
}

func TestPasskeyChallenge(t *testing.T) {
	now := time.Now()
	reg := PasskeyChallenge{UserID: "u1", ExpiresAt: now}
	login := PasskeyChallenge{ExpiresAt: now}
	if !reg.ForRegistrationBy("u1") || reg.ForRegistrationBy("u2") || reg.ForLogin() {
		t.Fatal("a registration challenge belongs to its account only")
	}
	if !login.ForLogin() || login.ForRegistrationBy("") {
		t.Fatal("a sign-in challenge can't register a passkey")
	}
	if !reg.Expired(now) || reg.Expired(now.Add(-time.Nanosecond)) {
		t.Fatal("challenge expiry boundary is wrong")
	}
}
