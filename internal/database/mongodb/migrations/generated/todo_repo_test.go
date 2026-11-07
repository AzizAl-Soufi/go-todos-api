package generated

import (
	"context"
	"errors"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
)

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
	_, getErr := repo.GetByID(ctx, "bad")

	for name, err := range map[string]error{
		"Create bad id":   repo.Create(ctx, &domain.Todo{ID: "bad"}),
		"Create bad user": repo.Create(ctx, &domain.Todo{UserID: "bad"}),
		"GetByID":         getErr,
		"Delete":          repo.Delete(ctx, "bad"),
	} {
		if !errors.Is(err, apperrors.ErrInvalidID) {
			t.Errorf("%s: err = %v, want ErrInvalidID", name, err)
		}
	}
}
