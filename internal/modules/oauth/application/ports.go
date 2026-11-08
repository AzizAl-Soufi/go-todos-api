// Package application holds the oauth module's ports. The use cases live in
// its two subpackages, following CQRS:
//
//	command/  writes: register clients, approve authorizations, exchange
//	          codes, refresh and revoke tokens, revoke connections
//	query/    reads: check an authorization request, authenticate access
//	          tokens, list a user's connections, get a client
package application

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
)

// Repositories return apperrors.ErrNotFound for missing records and
// apperrors.ErrInvalidID for IDs in the wrong format. Calls made with a
// context from tx.Runner.WithinTx must join that transaction.

// ---- write side (commands) ----

type ClientRepository interface {
	ClientReader
	// Create stores a registered client and assigns c.ID.
	Create(ctx context.Context, c *domain.Client) error
}

type CodeRepository interface {
	// Save stores c and deletes every code that expired before c.CreatedAt.
	Save(ctx context.Context, c *domain.Code) error
	// Consume atomically finds and deletes the code with hash.
	Consume(ctx context.Context, hash string) (*domain.Code, error)
	DeleteForUser(ctx context.Context, userID string) error
}

type GrantRepository interface {
	GrantReader
	// Create stores g and assigns g.ID.
	Create(ctx context.Context, g *domain.Grant) error
	// Rotate replaces a grant's tokens, only if its refresh token hash is
	// still r.RefreshHash: two refreshes with the same token can't both
	// succeed. ErrNotFound when the grant is gone or was rotated meanwhile.
	Rotate(ctx context.Context, r Rotation) error
	// Delete removes one of userID's grants; another user's is ErrNotFound.
	Delete(ctx context.Context, userID, id string) error
	// Revoke removes a grant whoever owns it (token revocation, reuse of an
	// old refresh token). A missing grant is not an error.
	Revoke(ctx context.Context, id string) error
	DeleteForUser(ctx context.Context, userID string) error
}

// Rotation is a refresh: the grant's new tokens.
type Rotation struct {
	GrantID          string
	RefreshHash      string // the refresh token being redeemed
	AccessHash       string
	AccessExpiresAt  time.Time
	NewRefreshHash   string
	RefreshExpiresAt time.Time
	UsedAt           time.Time
}

// ClientMetadataFetcher loads a Client ID Metadata Document: clientID is
// an HTTPS URL that serves the client's metadata. Failures are
// domain.ErrInvalidClient.
type ClientMetadataFetcher interface {
	Fetch(ctx context.Context, clientID string) (*domain.Client, error)
}

// Authenticator verifies the Firebase ID token the sign-in page posts and
// returns the account it belongs to (the identity module, through
// platform/auth: refused for unknown accounts and sign-ins before a
// log-out-everywhere).
type Authenticator = auth.Authenticator

// ---- read side (queries) ----

type ClientReader interface {
	Get(ctx context.Context, id string) (*domain.Client, error)
}

type GrantReader interface {
	Get(ctx context.Context, id string) (*domain.Grant, error)
	// ListForUser returns the user's grants, most recently used first.
	ListForUser(ctx context.Context, userID string) ([]*domain.Grant, error)
}
