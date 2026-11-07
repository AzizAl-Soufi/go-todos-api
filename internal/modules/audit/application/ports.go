// Package application holds the audit module's ports. The use cases live in
// its two subpackages, following CQRS:
//
//	command/  writes: record an event, delete a user's or a team's log
//	query/    reads: a team's audit log, a user's security log
package application

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/domain"
)

// Calls made with a context from tx.Runner.WithinTx must join that
// transaction, so an event is stored together with the change it records.

// EventRepository is the write side. Events are never changed, only appended
// and, with their user or team, deleted.
type EventRepository interface {
	// Append stores e and assigns e.ID.
	Append(ctx context.Context, e *domain.Event) error
	DeleteByUser(ctx context.Context, userID string) error
	DeleteByTeam(ctx context.Context, teamID string) error
}

type EventReader interface {
	// List returns at most f.Limit events matching f, ordered by
	// domain.Compare (newest first).
	List(ctx context.Context, f domain.Filter) ([]*domain.Event, error)
}

// EventStore is what a persistence adapter implements.
type EventStore interface {
	EventRepository
	EventReader
}
