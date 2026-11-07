package service

import (
	"context"
	"errors"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/middleware"
	"github.com/AzizAl-Soufi/go-todos-api/internal/repository/todos"
	"github.com/AzizAl-Soufi/go-todos-api/internal/repository/users"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
)

var errBoom = errors.New("boom")

// failingTodosRepo wraps the in-memory repo and lets tests inject errors.
type failingTodosRepo struct {
	todos.TodosRepository
	createErr, getAllErr, deleteByUserErr error
}

func (r *failingTodosRepo) Create(ctx context.Context, userID string, t *domain.Todo) error {
	if r.createErr != nil {
		return r.createErr
	}
	return r.TodosRepository.Create(ctx, userID, t)
}

func (r *failingTodosRepo) GetAll(ctx context.Context, userID string) ([]*domain.Todo, error) {
	if r.getAllErr != nil {
		return nil, r.getAllErr
	}
	return r.TodosRepository.GetAll(ctx, userID)
}

func (r *failingTodosRepo) DeleteByUserID(ctx context.Context, userID string) error {
	if r.deleteByUserErr != nil {
		return r.deleteByUserErr
	}
	return r.TodosRepository.DeleteByUserID(ctx, userID)
}

type failingUsersRepo struct {
	users.UsersRepository
	registerErr, deleteErr error
}

func (r *failingUsersRepo) Register(ctx context.Context, u *domain.UserDTO) (*domain.User, error) {
	if r.registerErr != nil {
		return nil, r.registerErr
	}
	return r.UsersRepository.Register(ctx, u)
}

func (r *failingUsersRepo) Delete(ctx context.Context, id string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	return r.UsersRepository.Delete(ctx, id)
}

type fixture struct {
	jwt       *middleware.JWTMiddleware
	usersRepo *failingUsersRepo
	todosRepo *failingTodosRepo
	users     UsersService
	todos     TodosService
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		jwt:       newTestJWTMiddleware(t),
		usersRepo: &failingUsersRepo{UsersRepository: users.NewInMemoryUsersRepository()},
		todosRepo: &failingTodosRepo{TodosRepository: todos.NewInMemoryTodosRepository()},
	}
	f.users = NewUsersService(f.usersRepo, f.todosRepo, f.jwt)
	f.todos = NewTodosService(f.todosRepo, f.usersRepo)
	return f
}

// register creates a user and returns an authenticated context for them.
func (f *fixture) register(t *testing.T, name, email string) (*domain.RegisterUserResponse, context.Context) {
	t.Helper()
	resp, err := f.users.Register(context.Background(), &domain.UserDTO{Name: name, Email: email})
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return resp, authContextFromAccessToken(t, f.jwt, resp.Authorization.AccessToken)
}

// ctxFor returns an authenticated context for an arbitrary (possibly unknown) user id.
func (f *fixture) ctxFor(t *testing.T, id string) context.Context {
	t.Helper()
	pair, err := f.jwt.GenerateTokenPair(middleware.NewAuthorization(id, "Ghost", "ghost@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	return authContextFromAccessToken(t, f.jwt, pair.AccessToken)
}

func TestServices_RejectMissingAuthorization(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	calls := map[string]func() error{
		"CreateTodo": func() error { _, err := f.todos.CreateTodo(ctx, &domain.CreateTodoDTO{Title: "x"}); return err },
		"GetTodo":    func() error { _, err := f.todos.GetTodo(ctx, "id"); return err },
		"UpdateTodo": func() error { _, err := f.todos.UpdateTodo(ctx, "id", &domain.UpdateTodoDTO{}); return err },
		"DeleteTodo": func() error { return f.todos.DeleteTodo(ctx, "id") },
		"GetTodos":   func() error { _, err := f.todos.GetTodos(ctx); return err },
		"Auth":       func() error { _, err := f.users.Auth(ctx); return err },
		"Overview":   func() error { _, err := f.users.GetOverview(ctx); return err },
		"Delete":     func() error { return f.users.DeleteAccount(ctx) },
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); err != middleware.ErrUnauthorizedContext {
				t.Fatalf("err = %v, want ErrUnauthorizedContext", err)
			}
		})
	}
}

func TestTodosService_UnknownUserIsUnauthorized(t *testing.T) {
	f := newFixture(t)
	ctx := f.ctxFor(t, "does-not-exist")

	if _, err := f.todos.CreateTodo(ctx, &domain.CreateTodoDTO{Title: "x"}); err != middleware.ErrUnauthorized {
		t.Fatalf("CreateTodo err = %v, want ErrUnauthorized", err)
	}
	if _, err := f.todos.GetTodos(ctx); err != middleware.ErrUnauthorized {
		t.Fatalf("GetTodos err = %v, want ErrUnauthorized", err)
	}
}

func TestTodosService_CreateSetsOwnerAndDefaults(t *testing.T) {
	f := newFixture(t)
	resp, ctx := f.register(t, "Ivy", "ivy@example.com")

	todo, err := f.todos.CreateTodo(ctx, &domain.CreateTodoDTO{Title: "x"})
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	if todo.ID == "" || todo.UserID != resp.Overview.ID || todo.Completed || todo.CreatedAt.IsZero() {
		t.Fatalf("unexpected todo: %+v", todo)
	}
}

func TestTodosService_CreatePropagatesRepoError(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.register(t, "Ivy", "ivy@example.com")
	f.todosRepo.createErr = errBoom

	todo, err := f.todos.CreateTodo(ctx, &domain.CreateTodoDTO{Title: "x"})
	if !errors.Is(err, errBoom) || todo != nil {
		t.Fatalf("got %v, %v; want nil, errBoom", todo, err)
	}
}

func TestTodosService_UpdateAndDeleteMissingTodo(t *testing.T) {
	f := newFixture(t)
	_, ctx := f.register(t, "Ivy", "ivy@example.com")

	if _, err := f.todos.UpdateTodo(ctx, "missing", &domain.UpdateTodoDTO{}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("UpdateTodo err = %v, want ErrNotFound", err)
	}
	if err := f.todos.DeleteTodo(ctx, "missing"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("DeleteTodo err = %v, want ErrNotFound", err)
	}
}

func TestTodosService_CannotModifyOtherUsersTodo(t *testing.T) {
	f := newFixture(t)
	_, owner := f.register(t, "Owner", "owner@example.com")
	_, intruder := f.register(t, "Intruder", "intruder@example.com")

	todo, err := f.todos.CreateTodo(owner, &domain.CreateTodoDTO{Title: "mine"})
	if err != nil {
		t.Fatal(err)
	}

	title := "hacked"
	if _, err := f.todos.UpdateTodo(intruder, todo.ID, &domain.UpdateTodoDTO{Title: &title}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("UpdateTodo err = %v, want ErrNotFound", err)
	}
	if err := f.todos.DeleteTodo(intruder, todo.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("DeleteTodo err = %v, want ErrNotFound", err)
	}
	if list, _ := f.todos.GetTodos(intruder); len(list) != 0 {
		t.Fatalf("intruder sees %d todos, want 0", len(list))
	}

	got, err := f.todos.GetTodo(owner, todo.ID)
	if err != nil || got.Title != "mine" {
		t.Fatalf("owner's todo changed: %+v, %v", got, err)
	}
}

func TestUsersService_RegisterExistingUserReturnsTheirTodos(t *testing.T) {
	f := newFixture(t)
	first, ctx := f.register(t, "Jo", "jo@example.com")
	if len(first.Overview.Todos) != 0 {
		t.Fatalf("new user should start with no todos, got %d", len(first.Overview.Todos))
	}
	if first.Overview.Todos == nil {
		t.Fatal("new user's todos should be an empty slice, not nil (serialises as [])")
	}

	if _, err := f.todos.CreateTodo(ctx, &domain.CreateTodoDTO{Title: "remember me"}); err != nil {
		t.Fatal(err)
	}

	again, _ := f.register(t, "Jo", "jo@example.com")
	if again.Overview.ID != first.Overview.ID {
		t.Fatalf("re-register returned a different user: %q vs %q", again.Overview.ID, first.Overview.ID)
	}
	if len(again.Overview.Todos) != 1 || again.Overview.Todos[0].Title != "remember me" {
		t.Fatalf("unexpected todos on re-register: %+v", again.Overview.Todos)
	}
}

func TestUsersService_RegisterErrors(t *testing.T) {
	t.Run("repo error", func(t *testing.T) {
		f := newFixture(t)
		f.usersRepo.registerErr = errBoom
		if _, err := f.users.Register(context.Background(), &domain.UserDTO{Name: "a", Email: "a@b.com"}); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v, want errBoom", err)
		}
	})

	t.Run("existing user, todos not found is tolerated", func(t *testing.T) {
		f := newFixture(t)
		f.register(t, "a", "a@b.com")
		f.todosRepo.getAllErr = apperrors.ErrNotFound

		resp, err := f.users.Register(context.Background(), &domain.UserDTO{Name: "a", Email: "a@b.com"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Overview.Todos == nil || len(resp.Overview.Todos) != 0 {
			t.Fatalf("expected empty todos, got %#v", resp.Overview.Todos)
		}
	})

	t.Run("existing user, todos error propagates", func(t *testing.T) {
		f := newFixture(t)
		f.register(t, "a", "a@b.com")
		f.todosRepo.getAllErr = errBoom

		if _, err := f.users.Register(context.Background(), &domain.UserDTO{Name: "a", Email: "a@b.com"}); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v, want errBoom", err)
		}
	})
}

func TestUsersService_RefreshTokenErrors(t *testing.T) {
	t.Run("invalid token", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.users.RefreshToken(context.Background(), &domain.RefreshRequest{RefreshToken: "garbage"})
		if err != middleware.ErrExpiredRefreshToken {
			t.Fatalf("err = %v, want ErrExpiredRefreshToken", err)
		}
	})

	t.Run("access token rejected", func(t *testing.T) {
		f := newFixture(t)
		resp, _ := f.register(t, "a", "a@b.com")
		_, err := f.users.RefreshToken(context.Background(), &domain.RefreshRequest{RefreshToken: resp.Authorization.AccessToken})
		if err != middleware.ErrInvalidRefreshTokenType {
			t.Fatalf("err = %v, want ErrInvalidRefreshTokenType", err)
		}
	})

	t.Run("deleted user", func(t *testing.T) {
		f := newFixture(t)
		resp, ctx := f.register(t, "a", "a@b.com")
		if err := f.users.DeleteAccount(ctx); err != nil {
			t.Fatal(err)
		}
		_, err := f.users.RefreshToken(context.Background(), &domain.RefreshRequest{RefreshToken: resp.Authorization.RefreshToken})
		if !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

func TestUsersService_OverviewAndAuth(t *testing.T) {
	type getter func(UsersService, context.Context) (*domain.Overview, error)
	getters := map[string]getter{
		"Auth":        func(s UsersService, c context.Context) (*domain.Overview, error) { return s.Auth(c) },
		"GetOverview": func(s UsersService, c context.Context) (*domain.Overview, error) { return s.GetOverview(c) },
	}

	for name, get := range getters {
		t.Run(name+"/includes todos", func(t *testing.T) {
			f := newFixture(t)
			_, ctx := f.register(t, "a", "a@b.com")
			f.todos.CreateTodo(ctx, &domain.CreateTodoDTO{Title: "one"})
			f.todos.CreateTodo(ctx, &domain.CreateTodoDTO{Title: "two"})

			ov, err := get(f.users, ctx)
			if err != nil {
				t.Fatal(err)
			}
			if ov.Name != "a" || ov.Email != "a@b.com" || len(ov.Todos) != 2 {
				t.Fatalf("unexpected overview: %+v", ov)
			}
		})

		t.Run(name+"/unknown user", func(t *testing.T) {
			f := newFixture(t)
			if _, err := get(f.users, f.ctxFor(t, "ghost")); !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})

		t.Run(name+"/todos not found tolerated", func(t *testing.T) {
			f := newFixture(t)
			_, ctx := f.register(t, "a", "a@b.com")
			f.todosRepo.getAllErr = apperrors.ErrNotFound

			ov, err := get(f.users, ctx)
			if err != nil {
				t.Fatal(err)
			}
			if ov.Todos == nil || len(ov.Todos) != 0 {
				t.Fatalf("expected empty todos, got %#v", ov.Todos)
			}
		})

		t.Run(name+"/todos error propagates", func(t *testing.T) {
			f := newFixture(t)
			_, ctx := f.register(t, "a", "a@b.com")
			f.todosRepo.getAllErr = errBoom

			if _, err := get(f.users, ctx); !errors.Is(err, errBoom) {
				t.Fatalf("err = %v, want errBoom", err)
			}
		})
	}
}

func TestUsersService_DeleteAccountErrors(t *testing.T) {
	t.Run("todos not found is tolerated", func(t *testing.T) {
		f := newFixture(t)
		_, ctx := f.register(t, "a", "a@b.com")
		f.todosRepo.deleteByUserErr = apperrors.ErrNotFound

		if err := f.users.DeleteAccount(ctx); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("todos error aborts before deleting user", func(t *testing.T) {
		f := newFixture(t)
		resp, ctx := f.register(t, "a", "a@b.com")
		f.todosRepo.deleteByUserErr = errBoom

		if err := f.users.DeleteAccount(ctx); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v, want errBoom", err)
		}
		if _, err := f.usersRepo.GetOverview(context.Background(), resp.Overview.ID); err != nil {
			t.Fatalf("user should still exist: %v", err)
		}
	})

	t.Run("user repo error propagates", func(t *testing.T) {
		f := newFixture(t)
		_, ctx := f.register(t, "a", "a@b.com")
		f.usersRepo.deleteErr = errBoom

		if err := f.users.DeleteAccount(ctx); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v, want errBoom", err)
		}
	})

	t.Run("deleting twice reports not found", func(t *testing.T) {
		f := newFixture(t)
		_, ctx := f.register(t, "a", "a@b.com")
		if err := f.users.DeleteAccount(ctx); err != nil {
			t.Fatal(err)
		}
		if err := f.users.DeleteAccount(ctx); !errors.Is(err, apperrors.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}
