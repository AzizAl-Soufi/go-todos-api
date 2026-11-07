// Package domain holds the identity entities (users, passkeys) and their
// invariants. Sign-in itself is Firebase Authentication's job; an account
// here is the API's record of a Firebase user. No HTTP, storage or crypto here.
package domain

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxNameLength  = 100
	MaxEmailLength = 254
)

type User struct {
	ID    string
	Name  string
	Email string
	// FirebaseUID links the account to its Firebase user. It is empty only
	// for accounts from before Firebase that haven't signed in since.
	FirebaseUID     string
	EmailVerifiedAt *time.Time
	// SessionsValidAfter revokes sign-ins: Firebase ID tokens of sign-ins
	// before it are refused (log out everywhere).
	SessionsValidAfter *time.Time
	CreatedAt          time.Time
}

// NewUser builds the account of a Firebase user. A blank name falls back to
// the local part of the email address.
func NewUser(firebaseUID, name, email string, emailVerified bool, now time.Time) (*User, error) {
	if firebaseUID == "" {
		return nil, ErrInvalidIDToken
	}
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		name, _, _ = strings.Cut(email, "@")
	}
	if name, err = NormalizeName(name); err != nil {
		return nil, err
	}
	u := &User{FirebaseUID: firebaseUID, Name: name, Email: email, CreatedAt: now.UTC()}
	if emailVerified {
		u.EmailVerifiedAt = &u.CreatedAt
	}
	return u, nil
}

func (u *User) EmailVerified() bool { return u.EmailVerifiedAt != nil }

// SignInRevoked reports whether a sign-in at authTime was revoked by logging
// out everywhere. Firebase's auth_time has second precision, so the cutoff
// is compared at seconds too.
func (u *User) SignInRevoked(authTime time.Time) bool {
	return u.SessionsValidAfter != nil && authTime.Before(u.SessionsValidAfter.Truncate(time.Second))
}

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
