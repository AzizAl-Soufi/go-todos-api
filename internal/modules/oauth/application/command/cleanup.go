package command

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
)

const (
	DefaultCleanupBatchSize = 500
	DefaultCleanupMaxPerRun = 50_000
)

// Cleanup deletes what can never be used again: expired authorization
// codes, grants whose refresh token expired, and registered clients older
// than domain.UnusedClientTTL that no grant or code references. It deletes
// in batches of BatchSize, at most MaxPerRun records of each kind per run
// (the next run continues). Running it twice is harmless.
type Cleanup struct {
	BatchSize int // 0: DefaultCleanupBatchSize
	MaxPerRun int // 0: DefaultCleanupMaxPerRun
}

type CleanupHandler struct{ base }

func (h CleanupHandler) Handle(ctx context.Context, cmd Cleanup) error {
	batch := orDefault(cmd.BatchSize, DefaultCleanupBatchSize)
	limit := orDefault(cmd.MaxPerRun, DefaultCleanupMaxPerRun)
	now := h.Now().UTC()

	codes, codesErr := deleteInBatches(ctx, batch, limit, func(n int) (int64, error) {
		return h.Codes.DeleteExpired(ctx, now, n)
	})
	// Grants before clients: a client whose last grant expired goes in the
	// same run.
	grants, grantsErr := deleteInBatches(ctx, batch, limit, func(n int) (int64, error) {
		return h.Grants.DeleteExpired(ctx, now, n)
	})
	clients, clientsErr := deleteInBatches(ctx, batch, limit, func(n int) (int64, error) {
		return h.Clients.DeleteUnused(ctx, now.Add(-domain.UnusedClientTTL), n)
	})

	slog.InfoContext(ctx, "oauth cleanup", "expired_codes", codes, "expired_grants", grants, "unused_clients", clients)
	return errors.Join(
		wrap("delete expired oauth codes", codesErr),
		wrap("delete expired oauth grants", grantsErr),
		wrap("delete unused oauth clients", clientsErr),
	)
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func wrap(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}

// deleteInBatches calls del with batch sizes until a batch comes back short
// or limit records are deleted, and returns how many were.
func deleteInBatches(ctx context.Context, batch, limit int, del func(n int) (int64, error)) (int64, error) {
	var total int64
	for total < int64(limit) {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n := min(batch, limit-int(total))
		deleted, err := del(n)
		total += deleted
		if err != nil {
			return total, err
		}
		if deleted < int64(n) {
			break
		}
	}
	return total, nil
}
