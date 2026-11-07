package domain

import "time"

// Session is one refresh token. Every login starts a family; each refresh
// revokes the current session and issues a successor in the same family.
// Presenting a revoked token means it leaked or was replayed, so the whole
// family is revoked.
type Session struct {
	ID        string
	UserID    string
	FamilyID  string
	TokenHash string // SHA-256 of the opaque token; the token itself is never stored
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

func (s *Session) Revoked() bool              { return s.RevokedAt != nil }
func (s *Session) Expired(now time.Time) bool { return !now.Before(s.ExpiresAt) }

// OneTimeToken is a pending, single-use token sent to the user by email: an
// email verification or a password reset. A user has at most one pending
// token of each kind; issuing a new one replaces the previous one.
type OneTimeToken struct {
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

func (v *OneTimeToken) Expired(now time.Time) bool { return !now.Before(v.ExpiresAt) }
