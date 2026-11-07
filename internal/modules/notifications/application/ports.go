// Package application holds the notifications module's ports. The use cases
// live in its two subpackages, following CQRS:
//
//	command/  writes: register and unregister devices, notify a team
//	query/    reads: the caller's devices
package application

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
)

// Repositories return apperrors.ErrNotFound for missing records and
// apperrors.ErrInvalidID for IDs in the wrong format.

type DeviceRepository interface {
	DeviceReader
	// Save stores d by token; a token already stored (for any user) is
	// replaced, so it moves to d.UserID and keeps its CreatedAt.
	Save(ctx context.Context, d *domain.Device) error
	// ListForUsers returns the devices of all the given users.
	ListForUsers(ctx context.Context, userIDs []string) ([]*domain.Device, error)
	// Delete removes one of userID's devices; another user's is ErrNotFound.
	Delete(ctx context.Context, userID, token string) error
	// DeleteTokens removes the given tokens, whoever they belong to (FCM
	// reported them invalid).
	DeleteTokens(ctx context.Context, tokens []string) error
	DeleteForUser(ctx context.Context, userID string) error
}

type DeviceReader interface {
	// ListForUser returns the user's devices, least recently registered first.
	ListForUser(ctx context.Context, userID string) ([]*domain.Device, error)
}

// Pusher delivers a message to devices (Firebase Cloud Messaging). It
// returns the tokens FCM rejected as no longer valid, which are then
// forgotten; other per-token failures are only reported in err.
type Pusher interface {
	Push(ctx context.Context, tokens []string, m domain.Message) (invalid []string, err error)
}

// Directory is how the module learns a team's members; the teams module
// implements it through platform/access.
type Directory = access.Directory
