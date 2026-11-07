package todos

// These tests cover the Mongo and Postgres repositories up to the point where
// they would talk to a database: ID parsing, no-op shortcuts, error mapping
// and row-to-domain mapping. Query behaviour needs integration tests against
// real databases.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/postgres/sqlc"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func assertInvalidID(t *testing.T, err error) {
	t.Helper()
	ae, ok := apperrors.From(err)
	if !ok || ae.Code() != "INVALID_ID" {
		t.Fatalf("err = %v, want INVALID_ID validation error", err)
	}
}

// ---- Mongo ----

func TestMongoTodos_RejectsInvalidIDs(t *testing.T) {
	// coll is nil: any call that reaches the database would panic.
	repo := &mongoTodoRepo{}
	ctx := context.Background()
	valid := bson.NewObjectID().Hex()

	tests := map[string]func() error{
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
		"DeleteByUserID bad": func() error { return repo.DeleteByUserID(ctx, "bad") },
		"GetAll uuid user":   func() error { _, err := repo.GetAll(ctx, uuid.NewString()); return err },
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) { assertInvalidID(t, call()) })
	}
}

func TestMongoTodos_UpdateWithNoFieldsIsNoop(t *testing.T) {
	repo := &mongoTodoRepo{} // nil coll proves no DB call is made
	id, user := bson.NewObjectID().Hex(), bson.NewObjectID().Hex()

	if err := repo.Update(context.Background(), id, user, &domain.UpdateTodoDTO{}); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestMongoTodos_ParseAndMap(t *testing.T) {
	oid := bson.NewObjectID()
	got, err := parseTodoID(oid.Hex())
	if err != nil || got != oid {
		t.Fatalf("parseTodoID = %v, %v", got, err)
	}

	userID := bson.NewObjectID()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	todo := toDomainTodo(&mongoTodo{ID: oid, UserID: userID, Title: "t", Completed: true, CreatedAt: created})

	want := domain.Todo{ID: oid.Hex(), UserID: userID.Hex(), Title: "t", Completed: true, CreatedAt: created}
	if *todo != want {
		t.Fatalf("toDomainTodo = %+v, want %+v", *todo, want)
	}
}

// ---- Postgres ----

func TestPostgresTodos_RejectsInvalidIDs(t *testing.T) {
	// A Queries over a nil DBTX: any call that reaches the database would panic.
	repo := &postgresTodoRepo{queries: sqlc.New(nil)}
	ctx := context.Background()
	valid := uuid.NewString()
	title := "x"

	tests := map[string]func() error{
		"Create bad user":    func() error { return repo.Create(ctx, "bad", &domain.Todo{Title: "x"}) },
		"Update bad id":      func() error { return repo.Update(ctx, "bad", valid, &domain.UpdateTodoDTO{Title: &title}) },
		"Update bad user":    func() error { return repo.Update(ctx, valid, "bad", &domain.UpdateTodoDTO{Title: &title}) },
		"Get bad id":         func() error { _, err := repo.Get(ctx, "bad", valid); return err },
		"Get bad user":       func() error { _, err := repo.Get(ctx, valid, "bad"); return err },
		"Delete bad id":      func() error { return repo.Delete(ctx, "bad", valid) },
		"Delete bad user":    func() error { return repo.Delete(ctx, valid, "bad") },
		"GetAll bad user":    func() error { _, err := repo.GetAll(ctx, "bad"); return err },
		"DeleteByUserID bad": func() error { return repo.DeleteByUserID(ctx, "bad") },
		"Get mongo id":       func() error { _, err := repo.Get(ctx, bson.NewObjectID().Hex(), valid); return err },
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) { assertInvalidID(t, call()) })
	}
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

func TestParseUUID(t *testing.T) {
	id := uuid.New()
	got, err := parseUUID(id.String())
	if err != nil || got != id {
		t.Fatalf("parseUUID = %v, %v", got, err)
	}

	got, err = parseUUID("")
	assertInvalidID(t, err)
	if got != uuid.Nil {
		t.Fatalf("parseUUID(\"\") = %v, want uuid.Nil", got)
	}
}

func TestMapTodoError(t *testing.T) {
	if err := mapTodoError("get", pgx.ErrNoRows); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("ErrNoRows mapped to %v, want ErrNotFound", err)
	}

	cause := errors.New("connection reset")
	err := mapTodoError("get todo", cause)
	if !errors.Is(err, cause) || err.Error() != "get todo: connection reset" {
		t.Fatalf("mapped error = %v", err)
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
