package generated

import (
	"context"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func assertInvalidID(t *testing.T, err error) {
	t.Helper()
	ae, ok := apperrors.From(err)
	if !ok || ae.Code() != "INVALID_ID" {
		t.Fatalf("err = %v, want INVALID_ID validation error", err)
	}
}

func TestParseTodoID(t *testing.T) {
	oid := bson.NewObjectID()
	got, err := parseTodoID(oid.Hex())
	if err != nil || got != oid {
		t.Fatalf("parseTodoID = %v, %v", got, err)
	}

	got, err = parseTodoID("bad")
	assertInvalidID(t, err)
	if got != bson.NilObjectID {
		t.Fatalf("got %v, want NilObjectID", got)
	}
}

func TestToDomainTodo(t *testing.T) {
	id, userID := bson.NewObjectID(), bson.NewObjectID()
	got := toDomainTodo(&mongoTodo{ID: id, UserID: userID, Title: "t"})

	want := domain.Todo{ID: id.Hex(), UserID: userID.Hex(), Title: "t"}
	if *got != want {
		t.Fatalf("got %+v, want %+v", *got, want)
	}
}

func TestRepo_RejectsInvalidIDsBeforeQuerying(t *testing.T) {
	repo := &mongoTodoRepo{} // nil coll: reaching the DB would panic
	ctx := context.Background()

	assertInvalidID(t, repo.Create(ctx, &domain.Todo{ID: "bad"}))
	assertInvalidID(t, repo.Create(ctx, &domain.Todo{UserID: "bad"}))
	_, err := repo.GetByID(ctx, "bad")
	assertInvalidID(t, err)
	assertInvalidID(t, repo.Delete(ctx, "bad"))
}
