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
		"  Ann@Example.COM ":   "ann@example.com",
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

func TestValidatePassword(t *testing.T) {
	cases := map[string]error{
		"1234567":               ErrWeakPassword,
		"12345678":              nil,
		strings.Repeat("x", 72): nil,
		strings.Repeat("x", 73): ErrPasswordTooLong,
		strings.Repeat("ü", 37): ErrPasswordTooLong, // 74 bytes: bcrypt's limit is in bytes
	}
	for pw, want := range cases {
		if err := ValidatePassword(pw); !errors.Is(err, want) && !(want == nil && err == nil) {
			t.Errorf("ValidatePassword(len %d) = %v, want %v", len(pw), err, want)
		}
	}
}

func TestNewUser(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("x", 3600))
	u, err := NewUser(" Ann ", "ANN@example.com", "hash", now)
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Ann" || u.Email != "ann@example.com" || u.PasswordHash != "hash" || u.CreatedAt.Location() != time.UTC {
		t.Fatalf("unexpected user: %+v", u)
	}
	if u.EmailVerified() || !u.HasPassword() || u.TokenVersion != 0 {
		t.Fatalf("unexpected state: %+v", u)
	}
}

func TestSessionAndVerificationExpiry(t *testing.T) {
	now := time.Now()
	s := Session{ExpiresAt: now}
	if !s.Expired(now) || s.Expired(now.Add(-time.Nanosecond)) {
		t.Fatal("session expiry boundary is wrong")
	}
	if s.Revoked() {
		t.Fatal("new session must not be revoked")
	}
	v := OneTimeToken{ExpiresAt: now}
	if !v.Expired(now) || v.Expired(now.Add(-time.Second)) {
		t.Fatal("verification expiry boundary is wrong")
	}
}
