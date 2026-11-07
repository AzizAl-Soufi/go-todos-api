package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxPasskeys is how many passkeys one account may register.
	MaxPasskeys          = 20
	MaxPasskeyNameLength = 64
	DefaultPasskeyName   = "Passkey"
)

// Passkey is a WebAuthn credential registered to an account. It signs the
// user in without a password.
type Passkey struct {
	ID     string
	UserID string
	Name   string
	// CredentialID is the authenticator's ID for the credential; unique
	// across all accounts.
	CredentialID []byte
	// Credential is the relying party's credential record (public key, sign
	// counter, flags). The domain treats it as opaque; only the WebAuthn
	// adapter reads it, and it changes on every sign-in.
	Credential []byte
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// NewPasskey builds a passkey for userID; a blank name gets the default one.
func NewPasskey(userID, name string, credentialID, credential []byte, now time.Time) (*Passkey, error) {
	name, err := NormalizePasskeyName(name)
	if err != nil {
		return nil, err
	}
	return &Passkey{
		UserID: userID, Name: name, CredentialID: credentialID, Credential: credential, CreatedAt: now.UTC(),
	}, nil
}

func NormalizePasskeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return DefaultPasskeyName, nil
	case utf8.RuneCountInString(name) > MaxPasskeyNameLength:
		return "", ErrPasskeyNameTooLong
	}
	return name, nil
}

// PasskeyChallenge is a pending WebAuthn ceremony: the challenge the server
// handed out and what it needs to check the answer. Only a hash of the token
// that names it is stored, and it can be redeemed once.
type PasskeyChallenge struct {
	TokenHash string
	// UserID is the account adding a passkey; empty for a sign-in, where the
	// passkey itself tells who the user is.
	UserID string
	// State is the ceremony state kept by the WebAuthn adapter; opaque here.
	State     []byte
	ExpiresAt time.Time
	CreatedAt time.Time
}

func (c *PasskeyChallenge) Expired(now time.Time) bool { return !now.Before(c.ExpiresAt) }

// ForRegistrationBy reports whether c was issued to userID to add a passkey.
func (c *PasskeyChallenge) ForRegistrationBy(userID string) bool {
	return c.UserID != "" && c.UserID == userID
}

// ForLogin reports whether c was issued for a passkey sign-in.
func (c *PasskeyChallenge) ForLogin() bool { return c.UserID == "" }
