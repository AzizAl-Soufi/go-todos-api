package memstore_test

import (
	"context"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Backend {
		todos, projects := memstore.NewTodos(), memstore.NewProjects()
		todos.LinkProjects(projects)
		return storetest.Backend{
			Todos:       todos,
			Projects:    projects,
			Collections: memstore.NewCollections(),
			Tx:          tx.None{},
			NewUser:     func(*testing.T) string { return bson.NewObjectID().Hex() },
			NewID:       func() string { return bson.NewObjectID().Hex() },
			MissingID:   "missing",
		}
	})
}

func TestTodos_CopiesValues(t *testing.T) {
	ctx := context.Background()
	s := memstore.NewTodos()
	todo := &domain.Todo{UserID: "u1", Title: "a", Tags: []string{"x"}}
	if err := s.Create(ctx, todo); err != nil {
		t.Fatal(err)
	}

	got, _ := s.Get(ctx, domain.Scope{UserID: "u1"}, todo.ID)
	got.Title, got.Tags[0] = "mutated", "mutated"
	todo.Title, todo.Tags[0] = "mutated", "mutated"
	if fresh, _ := s.Get(ctx, domain.Scope{UserID: "u1"}, todo.ID); fresh.Title != "a" || fresh.Tags[0] != "x" {
		t.Fatal("stored todo must not share memory with values handed out or passed in")
	}
}
