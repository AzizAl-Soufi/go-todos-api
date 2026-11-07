package users

// Covers the Mongo and Postgres user repositories up to the database boundary
// (ID parsing). Query behaviour needs integration tests against real databases.

import (
	"context"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/postgres/sqlc"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func assertInvalidID(t *testing.T, err error) {
	t.Helper()
	ae, ok := apperrors.From(err)
	if !ok || ae.Code() != "INVALID_ID" {
		t.Fatalf("err = %v, want INVALID_ID validation error", err)
	}
}

func TestMongoUsers_RejectsInvalidIDs(t *testing.T) {
	repo := &mongoUserRepo{} // nil coll: reaching the DB would panic
	ctx := context.Background()

	tests := map[string]func() error{
		"GetByID":          func() error { _, err := repo.GetByID(ctx, "bad"); return err },
		"Auth":             func() error { _, err := repo.Auth(ctx, "bad"); return err },
		"GetOverview":      func() error { _, err := repo.GetOverview(ctx, "bad"); return err },
		"Delete":           func() error { return repo.Delete(ctx, "bad") },
		"Auth empty":       func() error { _, err := repo.Auth(ctx, ""); return err },
		"Auth uuid format": func() error { _, err := repo.Auth(ctx, uuid.NewString()); return err },
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) { assertInvalidID(t, call()) })
	}
}

func TestParseUserID(t *testing.T) {
	oid := bson.NewObjectID()
	got, err := parseUserID(oid.Hex())
	if err != nil || got != oid {
		t.Fatalf("parseUserID = %v, %v", got, err)
	}

	got, err = parseUserID("zzz")
	assertInvalidID(t, err)
	if got != bson.NilObjectID {
		t.Fatalf("parseUserID(bad) = %v, want NilObjectID", got)
	}
}

func TestPostgresUsers_RejectsInvalidIDs(t *testing.T) {
	repo := &postgresUserRepo{queries: sqlc.New(nil)} // nil DBTX: reaching the DB would panic
	ctx := context.Background()

	tests := map[string]func() error{
		"Auth":           func() error { _, err := repo.Auth(ctx, "bad"); return err },
		"GetOverview":    func() error { _, err := repo.GetOverview(ctx, "bad"); return err },
		"Delete":         func() error { return repo.Delete(ctx, "bad") },
		"Auth mongo oid": func() error { _, err := repo.Auth(ctx, bson.NewObjectID().Hex()); return err },
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) { assertInvalidID(t, call()) })
	}
}
