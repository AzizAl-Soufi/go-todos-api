// Package application holds the identity module's ports. The use cases live
// in its two subpackages, following CQRS:
//
//	command/  writes: register, log in, rotate and revoke sessions, verify
//	          email, reset and change passwords, delete the account
//	query/    reads: the current user, the account overview, authentication
//
// Commands depend on the write ports, queries only on the narrow read ports.
package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
)

// Repositories return apperrors.ErrNotFound for missing records and
// apperrors.ErrInvalidID for IDs in the wrong format. Calls made with a
// context from tx.Runner.WithinTx must join that transaction.

// ---- write side (commands) ----

type UserRepository interface {
	UserReader
	// Create stores u and assigns u.ID; a duplicate email is domain.ErrEmailTaken.
	Create(ctx context.Context, u *domain.User) error
	FindByEmail(ctx context.Context, email string) (*domain.User, error)
	// MarkEmailVerified keeps the first verification time if already verified.
	MarkEmailVerified(ctx context.Context, id string, at time.Time) error
	IncrementTokenVersion(ctx context.Context, id string) error
	SetPasswordHash(ctx context.Context, id, hash string) error
	Delete(ctx context.Context, id string) error
}

type SessionRepository interface {
	// Create stores s and assigns s.ID.
	Create(ctx context.Context, s *domain.Session) error
	FindByTokenHash(ctx context.Context, tokenHash string) (*domain.Session, error)
	// Revoke marks an active session revoked and reports whether this call did
	// it; false means it was already revoked (e.g. a concurrent refresh won).
	Revoke(ctx context.Context, id string, at time.Time) (bool, error)
	RevokeFamily(ctx context.Context, familyID string, at time.Time) error
	RevokeAllForUser(ctx context.Context, userID string, at time.Time) error
	DeleteAllForUser(ctx context.Context, userID string) error
}

// OneTimeTokenRepository stores one kind of single-use token (email
// verifications, password resets), at most one pending per user.
type OneTimeTokenRepository interface {
	// Save stores v, replacing any pending token of the same user.
	Save(ctx context.Context, v *domain.OneTimeToken) error
	// Consume atomically finds and deletes the token with tokenHash.
	Consume(ctx context.Context, tokenHash string) (*domain.OneTimeToken, error)
	DeleteForUser(ctx context.Context, userID string) error
}

type PasskeyRepository interface {
	PasskeyReader
	// Create stores p and assigns p.ID; a credential ID that is already
	// stored (for any account) is domain.ErrPasskeyExists.
	Create(ctx context.Context, p *domain.Passkey) error
	// RecordUse stores the credential record as updated by a sign-in, and
	// when that sign-in happened.
	RecordUse(ctx context.Context, id string, credential []byte, at time.Time) error
	// Delete removes one of userID's passkeys; another user's passkey is
	// ErrNotFound.
	Delete(ctx context.Context, userID, id string) error
	DeleteAllForUser(ctx context.Context, userID string) error
}

// PasskeyChallengeRepository stores pending WebAuthn ceremonies.
type PasskeyChallengeRepository interface {
	// Save stores c and deletes every challenge that expired before c.CreatedAt.
	Save(ctx context.Context, c *domain.PasskeyChallenge) error
	// Consume atomically finds and deletes the challenge with tokenHash.
	Consume(ctx context.Context, tokenHash string) (*domain.PasskeyChallenge, error)
	DeleteForUser(ctx context.Context, userID string) error
}

// PasskeyUse is a verified passkey sign-in.
type PasskeyUse struct {
	CredentialID []byte
	// Credential is the credential record updated by this sign-in (sign
	// counter, flags); it must be stored back.
	Credential []byte
	// CloneWarning means the sign counter went backwards: a copy of the
	// private key may exist.
	CloneWarning bool
}

// PasskeyOwner returns the account a passkey sign-in names and its passkeys;
// ErrNotFound when there is no such account.
type PasskeyOwner func(userID string) (*domain.User, []*domain.Passkey, error)

// RelyingParty runs the WebAuthn ceremonies. Options are the JSON the client
// passes to navigator.credentials (create or get); state is what the server
// keeps until the answer comes back. Both are opaque to the application.
type RelyingParty interface {
	BeginRegistration(u *domain.User, existing []*domain.Passkey) (options json.RawMessage, state []byte, err error)
	// FinishRegistration verifies the client's attestation response; a
	// response that doesn't verify is domain.ErrPasskeyRejected.
	FinishRegistration(u *domain.User, existing []*domain.Passkey, state, response []byte) (credentialID, credential []byte, err error)
	// BeginLogin starts a sign-in with a discoverable credential: the
	// authenticator, not the user, says which account it belongs to.
	BeginLogin() (options json.RawMessage, state []byte, err error)
	// FinishLogin verifies the client's assertion response against the
	// passkeys of the account owner returns. A response that doesn't verify,
	// or names an unknown account or passkey, is
	// domain.ErrInvalidPasskeyCredential; the user is returned whenever the
	// account was found, so failures can be recorded in its security log.
	FinishLogin(state, response []byte, owner PasskeyOwner) (*domain.User, PasskeyUse, error)
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	// Compare returns nil only if password matches hash.
	Compare(hash, password string) error
}

type AccessTokenIssuer interface {
	Issue(u *domain.User, now time.Time) (token string, expiresAt time.Time, err error)
}

type Mailer interface {
	SendVerificationEmail(ctx context.Context, to, name, link string) error
	SendPasswordResetEmail(ctx context.Context, to, name, link string) error
}

// TeamMemberships is how account deletion reaches the teams module. It fails
// when the user still owns a team that has other members.
type TeamMemberships interface {
	RemoveUser(ctx context.Context, userID string) error
}

// AuditEntry is a security event about one user's account.
type AuditEntry struct {
	UserID  string // whose security log
	ActorID string // who did it; "" when unknown (a failed login)
	Action  string // e.g. "session.login"
	Details map[string]string
}

// AuditLog is how commands reach the audit module (the personal security log).
type AuditLog interface {
	Record(ctx context.Context, e AuditEntry) error
	DeleteUserLog(ctx context.Context, userID string) error
}

// TodoDeleter is how account deletion reaches the todos module.
type TodoDeleter interface {
	DeleteAllForUser(ctx context.Context, userID string) error
}

// ---- read side (queries) ----

type UserReader interface {
	FindByID(ctx context.Context, id string) (*domain.User, error)
}

type PasskeyReader interface {
	// ListForUser returns the user's passkeys, oldest first.
	ListForUser(ctx context.Context, userID string) ([]*domain.Passkey, error)
}

// AccessClaims is what an access token proves about its bearer.
type AccessClaims struct {
	UserID       string
	TokenVersion int
}

type AccessTokenVerifier interface {
	// Parse verifies a token and returns domain token errors on failure.
	Parse(token string) (AccessClaims, error)
}

// TodoSummary is the identity module's view of a todo, for the overview.
type TodoSummary struct {
	ID        string
	UserID    string
	Title     string
	Status    string
	Completed bool
	CreatedAt time.Time
}

// TodoLister is how the overview reads from the todos module.
type TodoLister interface {
	ListForUser(ctx context.Context, userID string) ([]TodoSummary, error)
}

// ---- adapters that serve both sides ----

// AccessTokens signs (commands) and verifies (queries) access tokens.
type AccessTokens interface {
	AccessTokenIssuer
	AccessTokenVerifier
}

// TodoGateway is the bridge to the todos module, for both sides.
type TodoGateway interface {
	TodoDeleter
	TodoLister
}
