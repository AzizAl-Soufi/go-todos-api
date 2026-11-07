// Package domain holds the identity entities (users, sessions, email
// verifications) and their invariants. No HTTP, storage or crypto here.
package domain

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxNameLength     = 100
	MaxEmailLength    = 254
	MinPasswordLength = 8
	// MaxPasswordBytes is bcrypt's input limit; longer passwords would be
	// silently truncated, so they are rejected instead.
	MaxPasswordBytes = 72
)

type User struct {
	ID              string
	Name            string
	Email           string
	PasswordHash    string
	EmailVerifiedAt *time.Time
	// TokenVersion is embedded in access tokens; bumping it revokes them all.
	TokenVersion int
	CreatedAt    time.Time
}

// NewUser builds a user from already-normalized name/email and a password hash.
func NewUser(name, email, passwordHash string, now time.Time) (*User, error) {
	name, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}
	email, err = NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	return &User{Name: name, Email: email, PasswordHash: passwordHash, CreatedAt: now.UTC()}, nil
}

func (u *User) EmailVerified() bool { return u.EmailVerifiedAt != nil }

// HasPassword is false for accounts created before password auth existed.
func (u *User) HasPassword() bool { return u.PasswordHash != "" }

func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", ErrInvalidName
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", ErrNameTooLong
	}
	return name, nil
}

// NormalizeEmail trims and lower-cases an address and requires a bare
// "local@domain" form (no display name).
func NormalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(email) > MaxEmailLength {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", ErrInvalidEmail
	}
	return email, nil
}

func ValidatePassword(password string) error {
	switch {
	case utf8.RuneCountInString(password) < MinPasswordLength:
		return ErrWeakPassword
	case len(password) > MaxPasswordBytes:
		return ErrPasswordTooLong
	}
	return nil
}
