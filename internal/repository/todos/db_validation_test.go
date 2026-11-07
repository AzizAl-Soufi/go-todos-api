package todos

// These tests cover the Mongo and Postgres repositories up to the point where
// they would talk to a database: ID validation, no-op shortcuts and row
// mapping. Query behaviour needs integration tests against real databases.
// ID parsing and error mapping themselves are tested in shared/ids and the
// database packages.

import (
	"context"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/postgres/sqlc"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/testutil"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// ---- Mongo ----

func TestMongoTodos_RejectsInvalidIDs(t *testing.T) {
	// coll is nil: any call that reaches the database would panic.
	repo := &mongoTodoRepo{}
	ctx := context.Background()
	valid := bson.NewObjectID().Hex()

	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, map[string]func() error{
		"Create bad user":    func() error { return repo.Create(ctx, "bad", &domain.Todo{ID: valid}) },
		"Create bad todo":    func() error { return repo.Create(ctx, valid, &domain.Todo{ID: "bad"}) },
		"Create empty todo":  func() error { return repo.Create(ctx, valid, &domain.Todo{}) },
		"Update bad id":      func() error { return repo.Update(ctx, "bad", valid, &domain.UpdateTodoDTO{}) },
		"Update bad user":    func() error { return repo.Update(ctx, valid, "bad", &domain.UpdateTodoDTO{}) },
		"Get bad id":         func() error { _, err := repo.Get(ctx, "bad", valid); return err },
		"Get bad user":       func() error { _, err := repo.Get(ctx, valid, "bad"); return err },
		"Delete bad id":      func() error { return repo.Delete(ctx, "bad", valid) },
		"Delete bad user":    func() error { return repo.Delete(ctx, valid, "bad") },
		"GetAll bad user":    func() error { _, err := repo.GetAll(ctx, "bad"); return err },
		"GetAll uuid user":   func() error { _, err := repo.GetAll(ctx, uuid.NewString()); return err },
		"DeleteByUserID bad": func() error { return repo.DeleteByUserID(ctx, "bad") },
	})
}

func TestMongoTodos_UpdateWithNoFieldsIsNoop(t *testing.T) {
	repo := &mongoTodoRepo{} // nil coll proves no DB call is made
	id, user := bson.NewObjectID().Hex(), bson.NewObjectID().Hex()

	if err := repo.Update(context.Background(), id, user, &domain.UpdateTodoDTO{}); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestToDomainTodo(t *testing.T) {
	id, userID := bson.NewObjectID(), bson.NewObjectID()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	got := toDomainTodo(&mongoTodo{ID: id, UserID: userID, Title: "t", Completed: true, CreatedAt: created})

	want := domain.Todo{ID: id.Hex(), UserID: userID.Hex(), Title: "t", Completed: true, CreatedAt: created}
	if *got != want {
		t.Fatalf("toDomainTodo = %+v, want %+v", *got, want)
	}
}

// ---- Postgres ----

func TestPostgresTodos_RejectsInvalidIDs(t *testing.T) {
	// A Queries over a nil DBTX: any call that reaches the database would panic.
	repo := &postgresTodoRepo{queries: sqlc.New(nil)}
	ctx := context.Background()
	valid := uuid.NewString()
	title := "x"

	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, map[string]func() error{
		"Create bad user":    func() error { return repo.Create(ctx, "bad", &domain.Todo{Title: "x"}) },
		"Update bad id":      func() error { return repo.Update(ctx, "bad", valid, &domain.UpdateTodoDTO{Title: &title}) },
		"Update bad user":    func() error { return repo.Update(ctx, valid, "bad", &domain.UpdateTodoDTO{Title: &title}) },
		"Get bad id":         func() error { _, err := repo.Get(ctx, "bad", valid); return err },
		"Get bad user":       func() error { _, err := repo.Get(ctx, valid, "bad"); return err },
		"Get mongo id":       func() error { _, err := repo.Get(ctx, bson.NewObjectID().Hex(), valid); return err },
		"Delete bad id":      func() error { return repo.Delete(ctx, "bad", valid) },
		"Delete bad user":    func() error { return repo.Delete(ctx, valid, "bad") },
		"GetAll bad user":    func() error { _, err := repo.GetAll(ctx, "bad"); return err },
		"DeleteByUserID bad": func() error { return repo.DeleteByUserID(ctx, "bad") },
	})
}

func TestPostgresTodos_UpdateWithNoFieldsIsNoop(t *testing.T) {
	repo := &postgresTodoRepo{queries: sqlc.New(nil)}
	ctx := context.Background()
	id, user := uuid.NewString(), uuid.NewString()

	if err := repo.Update(ctx, id, user, nil); err != nil {
		t.Fatalf("nil dto: err = %v, want nil", err)
	}
	if err := repo.Update(ctx, id, user, &domain.UpdateTodoDTO{}); err != nil {
		t.Fatalf("empty dto: err = %v, want nil", err)
	}
}

func TestPostgresToDomainTodo(t *testing.T) {
	id, userID := uuid.New(), uuid.New()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	got := postgresToDomainTodo(sqlc.Todo{ID: id, UserID: userID, Title: "t", Completed: true, CreatedAt: created})

	want := domain.Todo{ID: id.String(), UserID: userID.String(), Title: "t", Completed: true, CreatedAt: created}
	if *got != want {
		t.Fatalf("got %+v, want %+v", *got, want)
	}
}
