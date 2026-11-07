package users

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
)

func TestInMemoryUsersRepositoryReturnsCopies(t *testing.T) {
	repo := NewInMemoryUsersRepository()
	created, err := repo.Register(context.Background(), &domain.UserDTO{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("register user: %v", err)
	}

	stored, err := repo.GetOverview(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get overview: %v", err)
	}

	stored.Name = "Mallory"

	fresh, err := repo.GetOverview(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get overview after mutation: %v", err)
	}

	if fresh.Name != "Alice" {
		t.Fatalf("expected stored user name to remain unchanged, got %q", fresh.Name)
	}
}

func TestInMemoryUsers_RegisterNewUser(t *testing.T) {
	repo := NewInMemoryUsersRepository()

	u, err := repo.Register(context.Background(), &domain.UserDTO{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == "" || u.CreatedAt.IsZero() {
		t.Fatalf("expected ID and CreatedAt to be set: %+v", u)
	}
	if !u.IsNew {
		t.Fatal("expected IsNew=true for a first registration")
	}
	if u.Name != "Alice" || u.Email != "alice@example.com" {
		t.Fatalf("unexpected user: %+v", u)
	}
}

func TestInMemoryUsers_RegisterExistingEmailReturnsSameUser(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryUsersRepository()

	first, _ := repo.Register(ctx, &domain.UserDTO{Name: "Alice", Email: "alice@example.com"})
	second, err := repo.Register(ctx, &domain.UserDTO{Name: "Someone Else", Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}

	if second.ID != first.ID {
		t.Fatalf("ID = %q, want %q", second.ID, first.ID)
	}
	if second.IsNew {
		t.Fatal("expected IsNew=false for an existing email")
	}
	if second.Name != "Alice" {
		t.Fatalf("name = %q; existing user's name should be kept", second.Name)
	}
}

func TestInMemoryUsers_LookupsDoNotReportNew(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryUsersRepository()
	created, _ := repo.Register(ctx, &domain.UserDTO{Name: "Alice", Email: "alice@example.com"})

	for name, lookup := range map[string]func(context.Context, string) (*domain.User, error){
		"Auth":        repo.Auth,
		"GetOverview": repo.GetOverview,
	} {
		t.Run(name, func(t *testing.T) {
			u, err := lookup(ctx, created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if u.IsNew {
				t.Fatal("IsNew must only be true on the Register call that created the user")
			}
		})
	}
}

func TestInMemoryUsers_LookupMissing(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryUsersRepository()

	if _, err := repo.Auth(ctx, "missing"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Auth err = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetOverview(ctx, "missing"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("GetOverview err = %v, want ErrNotFound", err)
	}
}

func TestInMemoryUsers_Delete(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryUsersRepository()
	u, _ := repo.Register(ctx, &domain.UserDTO{Name: "Alice", Email: "alice@example.com"})

	if err := repo.Delete(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Auth(ctx, u.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleted user still found: %v", err)
	}
	if err := repo.Delete(ctx, u.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}

	again, _ := repo.Register(ctx, &domain.UserDTO{Name: "Alice", Email: "alice@example.com"})
	if !again.IsNew || again.ID == u.ID {
		t.Fatalf("re-registering a deleted email should create a new user: %+v", again)
	}
}

func TestCloneUser_Nil(t *testing.T) {
	if cloneUser(nil) != nil {
		t.Fatal("cloneUser(nil) should be nil")
	}
}

func TestInMemoryUsers_ConcurrentRegisterSameEmail(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryUsersRepository()

	ids := make([]string, 50)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, err := repo.Register(ctx, &domain.UserDTO{Name: fmt.Sprint(i), Email: "same@example.com"})
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = u.ID
		}()
	}
	wg.Wait()

	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("concurrent registration created multiple users: %v", ids)
		}
	}
}
