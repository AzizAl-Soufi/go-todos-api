package users

// Covers the Mongo and Postgres user repositories up to the database boundary.
// Query behaviour needs integration tests against real databases.

import (
	"context"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/postgres/sqlc"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/testutil"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoUsers_RejectsInvalidIDs(t *testing.T) {
	repo := &mongoUserRepo{} // nil coll: reaching the DB would panic
	ctx := context.Background()

	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, map[string]func() error{
		"GetByID":          func() error { _, err := repo.GetByID(ctx, "bad"); return err },
		"Auth":             func() error { _, err := repo.Auth(ctx, "bad"); return err },
		"GetOverview":      func() error { _, err := repo.GetOverview(ctx, "bad"); return err },
		"Delete":           func() error { return repo.Delete(ctx, "bad") },
		"Auth empty":       func() error { _, err := repo.Auth(ctx, ""); return err },
		"Auth uuid format": func() error { _, err := repo.Auth(ctx, uuid.NewString()); return err },
	})
}

func TestPostgresUsers_RejectsInvalidIDs(t *testing.T) {
	repo := &postgresUserRepo{queries: sqlc.New(nil)} // nil DBTX: reaching the DB would panic
	ctx := context.Background()

	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, map[string]func() error{
		"Auth":           func() error { _, err := repo.Auth(ctx, "bad"); return err },
		"GetOverview":    func() error { _, err := repo.GetOverview(ctx, "bad"); return err },
		"Delete":         func() error { return repo.Delete(ctx, "bad") },
		"Auth mongo oid": func() error { _, err := repo.Auth(ctx, bson.NewObjectID().Hex()); return err },
	})
}

func TestToDomainUser(t *testing.T) {
	id := uuid.New()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	row := sqlc.User{ID: id, Name: "Ann", Email: "ann@example.com", CreatedAt: created}

	for _, isNew := range []bool{true, false} {
		got := toDomainUser(row, isNew)
		if got.ID != id.String() || got.Name != "Ann" || got.Email != "ann@example.com" ||
			!got.CreatedAt.Equal(created) || got.IsNew != isNew {
			t.Fatalf("toDomainUser(isNew=%v) = %+v", isNew, got)
		}
	}
}
