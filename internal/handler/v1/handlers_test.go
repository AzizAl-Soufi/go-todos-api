package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/middleware"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
)

// ---- fakes ----

type fakeTodosService struct {
	todo  *domain.Todo
	todos []*domain.Todo
	err   error

	gotID     string
	gotCreate *domain.CreateTodoDTO
	gotUpdate *domain.UpdateTodoDTO
	calls     int
}

func (f *fakeTodosService) CreateTodo(_ context.Context, dto *domain.CreateTodoDTO) (*domain.Todo, error) {
	f.calls++
	f.gotCreate = dto
	return f.todo, f.err
}
func (f *fakeTodosService) GetTodo(_ context.Context, id string) (*domain.Todo, error) {
	f.calls++
	f.gotID = id
	return f.todo, f.err
}
func (f *fakeTodosService) UpdateTodo(_ context.Context, id string, dto *domain.UpdateTodoDTO) (*domain.Todo, error) {
	f.calls++
	f.gotID, f.gotUpdate = id, dto
	return f.todo, f.err
}
func (f *fakeTodosService) DeleteTodo(_ context.Context, id string) error {
	f.calls++
	f.gotID = id
	return f.err
}
func (f *fakeTodosService) GetTodos(context.Context) ([]*domain.Todo, error) {
	f.calls++
	return f.todos, f.err
}

type fakeUsersService struct {
	register *domain.RegisterUserResponse
	overview *domain.Overview
	tokens   *middleware.TokenPair
	err      error

	gotUser    *domain.UserDTO
	gotRefresh *domain.RefreshRequest
	calls      int
}

func (f *fakeUsersService) Register(_ context.Context, u *domain.UserDTO) (*domain.RegisterUserResponse, error) {
	f.calls++
	f.gotUser = u
	return f.register, f.err
}
func (f *fakeUsersService) Auth(context.Context) (*domain.Overview, error) {
	f.calls++
	return f.overview, f.err
}
func (f *fakeUsersService) RefreshToken(_ context.Context, p *domain.RefreshRequest) (*middleware.TokenPair, error) {
	f.calls++
	f.gotRefresh = p
	return f.tokens, f.err
}
func (f *fakeUsersService) GetOverview(context.Context) (*domain.Overview, error) {
	f.calls++
	return f.overview, f.err
}
func (f *fakeUsersService) DeleteAccount(context.Context) error {
	f.calls++
	return f.err
}

// ---- helpers ----

// serve routes the request through a ServeMux so r.PathValue("id") is populated.
func serve(t *testing.T, pattern string, h http.HandlerFunc, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(pattern, h)

	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return v
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, status, rec.Body)
	}
	if got := decode[map[string]string](t, rec)["error"]; got != message {
		t.Fatalf("error = %q, want %q", got, message)
	}
}

var errPlain = errors.New("something broke")

// ---- todos handler ----

func TestTodosHandler_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &fakeTodosService{todo: &domain.Todo{ID: "t1", Title: "Buy milk"}}
		h := NewTodosHandler(svc)

		rec := serve(t, "POST /todos", h.Create, "POST", "/todos", `{"title":" Buy milk "}`)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", rec.Code)
		}
		if svc.gotCreate.Title != "Buy milk" {
			t.Fatalf("service got title %q, want trimmed", svc.gotCreate.Title)
		}
		if got := decode[domain.Todo](t, rec); got.ID != "t1" {
			t.Fatalf("response id = %q, want t1", got.ID)
		}
	})

	t.Run("invalid body does not reach service", func(t *testing.T) {
		svc := &fakeTodosService{}
		rec := serve(t, "POST /todos", NewTodosHandler(svc).Create, "POST", "/todos", `{"title":""}`)

		assertError(t, rec, http.StatusBadRequest, "invalid title value")
		if svc.calls != 0 {
			t.Fatal("service must not be called on validation failure")
		}
	})

	t.Run("app error from service", func(t *testing.T) {
		svc := &fakeTodosService{err: middleware.ErrUnauthorized}
		rec := serve(t, "POST /todos", NewTodosHandler(svc).Create, "POST", "/todos", `{"title":"x"}`)
		assertError(t, rec, http.StatusUnauthorized, "unauthorized request")
	})

	t.Run("plain error from service", func(t *testing.T) {
		svc := &fakeTodosService{err: errPlain}
		rec := serve(t, "POST /todos", NewTodosHandler(svc).Create, "POST", "/todos", `{"title":"x"}`)
		assertError(t, rec, http.StatusInternalServerError, errPlain.Error())
	})
}

func TestTodosHandler_Get(t *testing.T) {
	t.Run("success passes path id", func(t *testing.T) {
		svc := &fakeTodosService{todo: &domain.Todo{ID: "abc"}}
		rec := serve(t, "GET /todos/{id}", NewTodosHandler(svc).Get, "GET", "/todos/abc", "")

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if svc.gotID != "abc" {
			t.Fatalf("service got id %q, want abc", svc.gotID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		svc := &fakeTodosService{err: apperrors.ErrNotFound}
		rec := serve(t, "GET /todos/{id}", NewTodosHandler(svc).Get, "GET", "/todos/abc", "")
		assertError(t, rec, http.StatusNotFound, "entity not found")
	})

	t.Run("plain error", func(t *testing.T) {
		svc := &fakeTodosService{err: errPlain}
		rec := serve(t, "GET /todos/{id}", NewTodosHandler(svc).Get, "GET", "/todos/abc", "")
		assertError(t, rec, http.StatusInternalServerError, errPlain.Error())
	})
}

func TestTodosHandler_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &fakeTodosService{todo: &domain.Todo{ID: "abc", Completed: true}}
		rec := serve(t, "PATCH /todos/{id}", NewTodosHandler(svc).Update, "PATCH", "/todos/abc", `{"completed":true}`)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if svc.gotID != "abc" || svc.gotUpdate.Completed == nil || !*svc.gotUpdate.Completed {
			t.Fatalf("service got id=%q dto=%+v", svc.gotID, svc.gotUpdate)
		}
		if got := decode[domain.Todo](t, rec); !got.Completed {
			t.Fatal("expected completed todo in response")
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		svc := &fakeTodosService{}
		rec := serve(t, "PATCH /todos/{id}", NewTodosHandler(svc).Update, "PATCH", "/todos/abc", `{}`)
		assertError(t, rec, http.StatusBadRequest, "invalid request body")
		if svc.calls != 0 {
			t.Fatal("service must not be called on validation failure")
		}
	})

	t.Run("service app error", func(t *testing.T) {
		svc := &fakeTodosService{err: apperrors.ErrNotFound}
		rec := serve(t, "PATCH /todos/{id}", NewTodosHandler(svc).Update, "PATCH", "/todos/abc", `{"completed":true}`)
		assertError(t, rec, http.StatusNotFound, "entity not found")
	})

	t.Run("service plain error", func(t *testing.T) {
		svc := &fakeTodosService{err: errPlain}
		rec := serve(t, "PATCH /todos/{id}", NewTodosHandler(svc).Update, "PATCH", "/todos/abc", `{"completed":true}`)
		assertError(t, rec, http.StatusInternalServerError, errPlain.Error())
	})
}

func TestTodosHandler_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &fakeTodosService{}
		rec := serve(t, "DELETE /todos/{id}", NewTodosHandler(svc).Delete, "DELETE", "/todos/abc", "")

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if svc.gotID != "abc" {
			t.Fatalf("service got id %q, want abc", svc.gotID)
		}
		if body := strings.TrimSpace(rec.Body.String()); body != "{}" {
			t.Fatalf("body = %q, want {}", body)
		}
	})

	t.Run("app error", func(t *testing.T) {
		svc := &fakeTodosService{err: apperrors.ErrNotFound}
		rec := serve(t, "DELETE /todos/{id}", NewTodosHandler(svc).Delete, "DELETE", "/todos/abc", "")
		assertError(t, rec, http.StatusNotFound, "entity not found")
	})

	t.Run("plain error", func(t *testing.T) {
		svc := &fakeTodosService{err: errPlain}
		rec := serve(t, "DELETE /todos/{id}", NewTodosHandler(svc).Delete, "DELETE", "/todos/abc", "")
		assertError(t, rec, http.StatusInternalServerError, errPlain.Error())
	})
}

func TestTodosHandler_GetAll(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &fakeTodosService{todos: []*domain.Todo{{ID: "1"}, {ID: "2"}}}
		rec := serve(t, "GET /todos", NewTodosHandler(svc).GettAll, "GET", "/todos", "")

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := decode[[]domain.Todo](t, rec); len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
	})

	t.Run("app error", func(t *testing.T) {
		svc := &fakeTodosService{err: middleware.ErrUnauthorizedContext}
		rec := serve(t, "GET /todos", NewTodosHandler(svc).GettAll, "GET", "/todos", "")
		assertError(t, rec, http.StatusUnauthorized, middleware.ErrUnauthorizedContext.Message())
	})

	t.Run("plain error", func(t *testing.T) {
		svc := &fakeTodosService{err: errPlain}
		rec := serve(t, "GET /todos", NewTodosHandler(svc).GettAll, "GET", "/todos", "")
		assertError(t, rec, http.StatusInternalServerError, errPlain.Error())
	})
}

// ---- users handler ----

func TestUsersHandler_Register(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &fakeUsersService{register: &domain.RegisterUserResponse{
			Overview:      &domain.Overview{ID: "u1", Name: "Alice"},
			Authorization: &middleware.TokenPair{AccessToken: "a", RefreshToken: "r"},
		}}
		rec := serve(t, "POST /users", NewUsersHandler(svc).Register, "POST", "/users", `{"name":"Alice","email":"alice@example.com"}`)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", rec.Code)
		}
		if svc.gotUser.Email != "alice@example.com" {
			t.Fatalf("service got %+v", svc.gotUser)
		}
		got := decode[domain.RegisterUserResponse](t, rec)
		if got.Overview.ID != "u1" || got.Authorization.AccessToken != "a" {
			t.Fatalf("unexpected response: %+v", got)
		}
	})

	t.Run("invalid body", func(t *testing.T) {
		svc := &fakeUsersService{}
		rec := serve(t, "POST /users", NewUsersHandler(svc).Register, "POST", "/users", `{"name":"Alice","email":"nope"}`)
		assertError(t, rec, http.StatusBadRequest, "invalid email")
		if svc.calls != 0 {
			t.Fatal("service must not be called on validation failure")
		}
	})

	t.Run("app error", func(t *testing.T) {
		svc := &fakeUsersService{err: apperrors.ErrExistingUser}
		rec := serve(t, "POST /users", NewUsersHandler(svc).Register, "POST", "/users", `{"name":"A","email":"a@b.com"}`)
		assertError(t, rec, http.StatusConflict, "user already exists")
	})

	t.Run("plain error", func(t *testing.T) {
		svc := &fakeUsersService{err: errPlain}
		rec := serve(t, "POST /users", NewUsersHandler(svc).Register, "POST", "/users", `{"name":"A","email":"a@b.com"}`)
		assertError(t, rec, http.StatusInternalServerError, errPlain.Error())
	})
}

func TestUsersHandler_RefreshToken(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &fakeUsersService{tokens: &middleware.TokenPair{AccessToken: "a2", RefreshToken: "r2"}}
		rec := serve(t, "POST /refresh", NewUsersHandler(svc).RefreshToken, "POST", "/refresh", `{"refresh_token":"r1"}`)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", rec.Code)
		}
		if svc.gotRefresh.RefreshToken != "r1" {
			t.Fatalf("service got %+v", svc.gotRefresh)
		}
		if got := decode[middleware.TokenPair](t, rec); got.AccessToken != "a2" {
			t.Fatalf("unexpected response: %+v", got)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		svc := &fakeUsersService{}
		rec := serve(t, "POST /refresh", NewUsersHandler(svc).RefreshToken, "POST", "/refresh", `{}`)
		assertError(t, rec, http.StatusBadRequest, "Refresh token is required")
		if svc.calls != 0 {
			t.Fatal("service must not be called on validation failure")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		svc := &fakeUsersService{err: middleware.ErrExpiredRefreshToken}
		rec := serve(t, "POST /refresh", NewUsersHandler(svc).RefreshToken, "POST", "/refresh", `{"refresh_token":"r1"}`)
		assertError(t, rec, http.StatusUnauthorized, middleware.ErrExpiredRefreshToken.Message())
	})

	t.Run("plain error", func(t *testing.T) {
		svc := &fakeUsersService{err: errPlain}
		rec := serve(t, "POST /refresh", NewUsersHandler(svc).RefreshToken, "POST", "/refresh", `{"refresh_token":"r1"}`)
		assertError(t, rec, http.StatusInternalServerError, errPlain.Error())
	})
}

func TestUsersHandler_OverviewEndpoints(t *testing.T) {
	overview := &domain.Overview{ID: "u1", Name: "Alice", Todos: []*domain.Todo{}}

	tests := []struct {
		name       string
		handler    func(*UsersHandler) http.HandlerFunc
		wantStatus int
	}{
		// Auth currently responds 201 Created; GetOverview responds 200 OK.
		{"Auth", func(h *UsersHandler) http.HandlerFunc { return h.Auth }, http.StatusCreated},
		{"GetOverview", func(h *UsersHandler) http.HandlerFunc { return h.GetOverview }, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/success", func(t *testing.T) {
			svc := &fakeUsersService{overview: overview}
			rec := serve(t, "GET /me", tt.handler(NewUsersHandler(svc)), "GET", "/me", "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := decode[domain.Overview](t, rec); got.ID != "u1" {
				t.Fatalf("unexpected response: %+v", got)
			}
		})
		t.Run(tt.name+"/app error", func(t *testing.T) {
			svc := &fakeUsersService{err: apperrors.ErrNotFound}
			rec := serve(t, "GET /me", tt.handler(NewUsersHandler(svc)), "GET", "/me", "")
			assertError(t, rec, http.StatusNotFound, "entity not found")
		})
		t.Run(tt.name+"/plain error", func(t *testing.T) {
			svc := &fakeUsersService{err: errPlain}
			rec := serve(t, "GET /me", tt.handler(NewUsersHandler(svc)), "GET", "/me", "")
			assertError(t, rec, http.StatusInternalServerError, errPlain.Error())
		})
	}
}

func TestUsersHandler_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &fakeUsersService{}
		rec := serve(t, "DELETE /me", NewUsersHandler(svc).Delete, "DELETE", "/me", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if svc.calls != 1 {
			t.Fatalf("service calls = %d, want 1", svc.calls)
		}
	})

	t.Run("app error", func(t *testing.T) {
		svc := &fakeUsersService{err: middleware.ErrUnauthorizedContext}
		rec := serve(t, "DELETE /me", NewUsersHandler(svc).Delete, "DELETE", "/me", "")
		assertError(t, rec, http.StatusUnauthorized, middleware.ErrUnauthorizedContext.Message())
	})

	t.Run("plain error", func(t *testing.T) {
		svc := &fakeUsersService{err: errPlain}
		rec := serve(t, "DELETE /me", NewUsersHandler(svc).Delete, "DELETE", "/me", "")
		assertError(t, rec, http.StatusInternalServerError, errPlain.Error())
	})
}
