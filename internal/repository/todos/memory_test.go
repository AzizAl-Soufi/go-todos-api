package todos

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
)

func TestInMemoryTodosRepositoryReturnsCopies(t *testing.T) {
	repo := NewInMemoryTodosRepository()
	created := &domain.Todo{ID: "todo-1", UserID: "user-1", Title: "Write tests", CreatedAt: time.Now().UTC()}
	if err := repo.Create(context.Background(), "user-1", created); err != nil {
		t.Fatalf("create todo: %v", err)
	}

	stored, err := repo.Get(context.Background(), "todo-1", "user-1")
	if err != nil {
		t.Fatalf("get todo: %v", err)
	}

	stored.Title = "Steal state"

	fresh, err := repo.Get(context.Background(), "todo-1", "user-1")
	if err != nil {
		t.Fatalf("get todo after mutation: %v", err)
	}

	if fresh.Title != "Write tests" {
		t.Fatalf("expected stored todo title to remain unchanged, got %q", fresh.Title)
	}
}

func TestInMemoryTodos_CreateDoesNotAliasInput(t *testing.T) {
	repo := NewInMemoryTodosRepository()
	input := &domain.Todo{ID: "t1", Title: "original"}
	if err := repo.Create(context.Background(), "u1", input); err != nil {
		t.Fatal(err)
	}

	input.Title = "mutated after create"

	got, _ := repo.Get(context.Background(), "t1", "u1")
	if got.Title != "original" {
		t.Fatalf("stored title = %q, want original", got.Title)
	}
}

func TestInMemoryTodos_CreateFillsDefaultsAndOwner(t *testing.T) {
	repo := NewInMemoryTodosRepository().(*inMemoryTodosRepo)
	ctx := context.Background()

	if err := repo.Create(ctx, "owner", &domain.Todo{UserID: "spoofed", Title: "x"}); err != nil {
		t.Fatal(err)
	}

	all, _ := repo.GetAll(ctx, "owner")
	if len(all) != 1 {
		t.Fatalf("len = %d, want 1", len(all))
	}
	got := all[0]
	if got.ID == "" {
		t.Error("expected an ID to be generated")
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if got.UserID != "owner" {
		t.Errorf("UserID = %q, want owner (userID argument wins)", got.UserID)
	}
	if spoofed, _ := repo.GetAll(ctx, "spoofed"); len(spoofed) != 0 {
		t.Error("todo must not be attributed to the UserID on the input")
	}
}

func TestInMemoryTodos_CreateDuplicateID(t *testing.T) {
	repo := NewInMemoryTodosRepository()
	ctx := context.Background()

	if err := repo.Create(ctx, "u1", &domain.Todo{ID: "t1"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, "u2", &domain.Todo{ID: "t1"}); !errors.Is(err, apperrors.ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
}

func TestInMemoryTodos_GetEnforcesOwnership(t *testing.T) {
	repo := NewInMemoryTodosRepository()
	ctx := context.Background()
	repo.Create(ctx, "u1", &domain.Todo{ID: "t1"})

	if _, err := repo.Get(ctx, "t1", "u2"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("other user: err = %v, want ErrNotFound", err)
	}
	if _, err := repo.Get(ctx, "missing", "u1"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("missing: err = %v, want ErrNotFound", err)
	}
}

func TestInMemoryTodos_Update(t *testing.T) {
	ctx := context.Background()
	title, done := "new", true

	t.Run("applies partial update", func(t *testing.T) {
		repo := NewInMemoryTodosRepository()
		repo.Create(ctx, "u1", &domain.Todo{ID: "t1", Title: "old"})

		if err := repo.Update(ctx, "t1", "u1", &domain.UpdateTodoDTO{Completed: &done}); err != nil {
			t.Fatal(err)
		}
		got, _ := repo.Get(ctx, "t1", "u1")
		if got.Title != "old" || !got.Completed {
			t.Fatalf("unexpected todo: %+v", got)
		}

		if err := repo.Update(ctx, "t1", "u1", &domain.UpdateTodoDTO{Title: &title}); err != nil {
			t.Fatal(err)
		}
		got, _ = repo.Get(ctx, "t1", "u1")
		if got.Title != "new" || !got.Completed {
			t.Fatalf("unexpected todo: %+v", got)
		}
	})

	t.Run("other user cannot update", func(t *testing.T) {
		repo := NewInMemoryTodosRepository()
		repo.Create(ctx, "u1", &domain.Todo{ID: "t1", Title: "old"})

		if err := repo.Update(ctx, "t1", "u2", &domain.UpdateTodoDTO{Title: &title}); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		got, _ := repo.Get(ctx, "t1", "u1")
		if got.Title != "old" {
			t.Fatalf("todo was modified: %+v", got)
		}
	})

	t.Run("missing todo", func(t *testing.T) {
		repo := NewInMemoryTodosRepository()
		if err := repo.Update(ctx, "nope", "u1", &domain.UpdateTodoDTO{Title: &title}); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestInMemoryTodos_Delete(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryTodosRepository()
	repo.Create(ctx, "u1", &domain.Todo{ID: "t1"})

	if err := repo.Delete(ctx, "t1", "u2"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("other user: err = %v, want ErrNotFound", err)
	}
	if _, err := repo.Get(ctx, "t1", "u1"); err != nil {
		t.Fatal("todo must survive a delete by another user")
	}

	if err := repo.Delete(ctx, "t1", "u1"); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if err := repo.Delete(ctx, "t1", "u1"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("second delete: err = %v, want ErrNotFound", err)
	}
}

func TestInMemoryTodos_GetAllAndDeleteByUserID(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryTodosRepository()
	for i := range 3 {
		repo.Create(ctx, "u1", &domain.Todo{ID: fmt.Sprintf("a%d", i)})
	}
	repo.Create(ctx, "u2", &domain.Todo{ID: "b0"})

	if got, _ := repo.GetAll(ctx, "u1"); len(got) != 3 {
		t.Fatalf("u1 todos = %d, want 3", len(got))
	}

	empty, err := repo.GetAll(ctx, "nobody")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("GetAll(nobody) = %#v, %v; want empty non-nil slice", empty, err)
	}

	if err := repo.DeleteByUserID(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetAll(ctx, "u1"); len(got) != 0 {
		t.Fatalf("u1 todos after delete = %d, want 0", len(got))
	}
	if got, _ := repo.GetAll(ctx, "u2"); len(got) != 1 {
		t.Fatalf("u2 todos = %d, want 1 (must be untouched)", len(got))
	}

	// Deleting for a user with no todos is not an error in memory.
	if err := repo.DeleteByUserID(ctx, "nobody"); err != nil {
		t.Fatalf("DeleteByUserID(nobody) = %v, want nil", err)
	}
}

func TestInMemoryTodos_ExtraLookups(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryTodosRepository().(*inMemoryTodosRepo)
	repo.Create(ctx, "u1", &domain.Todo{ID: "t1", Title: "x"})

	got, err := repo.GetByID(ctx, "t1")
	if err != nil || got.Title != "x" {
		t.Fatalf("GetByID = %+v, %v", got, err)
	}
	got.Title = "mutated"
	if again, _ := repo.GetByID(ctx, "t1"); again.Title != "x" {
		t.Fatal("GetByID must return a copy")
	}
	if _, err := repo.GetByID(ctx, "missing"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("GetByID(missing) err = %v, want ErrNotFound", err)
	}

	list, err := repo.GetByUserID(ctx, "u1")
	if err != nil || len(list) != 1 {
		t.Fatalf("GetByUserID = %v, %v", list, err)
	}
}

func TestCloneTodo_Nil(t *testing.T) {
	if cloneTodo(nil) != nil {
		t.Fatal("cloneTodo(nil) should be nil")
	}
}

func TestInMemoryTodos_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryTodosRepository()

	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("t%d", i)
			repo.Create(ctx, "u1", &domain.Todo{ID: id})
			repo.Get(ctx, id, "u1")
			repo.GetAll(ctx, "u1")
		}()
	}
	wg.Wait()

	if got, _ := repo.GetAll(ctx, "u1"); len(got) != 50 {
		t.Fatalf("len = %d, want 50", len(got))
	}
}
