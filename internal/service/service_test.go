package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/middleware"
	"github.com/AzizAl-Soufi/go-todos-api/internal/repository/todos"
	"github.com/AzizAl-Soufi/go-todos-api/internal/repository/users"
)

// testKey is generated once per test binary; RSA key generation is slow and
// tests must not depend on the (gitignored) keys in secrets/.
var testKey = sync.OnceValues(func() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
})

func newTestJWTMiddleware(t *testing.T) *middleware.JWTMiddleware {
	t.Helper()

	key, err := testKey()
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	dir := t.TempDir()
	jwtCfg := &config.JWTConfig{
		PrivateKeyPath: filepath.Join(dir, "private.pem"),
		PublicKeyPath:  filepath.Join(dir, "public.pem"),
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(jwtCfg.PrivateKeyPath, privPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jwtCfg.PublicKeyPath, pubPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	jwt, err := middleware.NewJWTMiddleware(jwtCfg)
	if err != nil {
		t.Fatalf("new jwt middleware: %v", err)
	}

	return jwt
}

func authContextFromAccessToken(t *testing.T, jwt *middleware.JWTMiddleware, token string) context.Context {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/auth", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()

	var ctx context.Context
	jwt.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(res, req)

	if ctx == nil {
		t.Fatal("expected authorization context to be set")
	}

	return ctx
}

func TestUsersService_RegisterAndOverview(t *testing.T) {
	jwt := newTestJWTMiddleware(t)
	usersSvc := NewUsersService(users.NewInMemoryUsersRepository(), todos.NewInMemoryTodosRepository(), jwt)

	resp, err := usersSvc.Register(context.Background(), &domain.UserDTO{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("register user: %v", err)
	}

	if resp == nil || resp.Overview == nil {
		t.Fatal("expected response and overview")
	}
	if resp.Overview.Name != "Alice" {
		t.Fatalf("expected overview name Alice, got %q", resp.Overview.Name)
	}
	if resp.Authorization == nil || resp.Authorization.AccessToken == "" {
		t.Fatal("expected authorization token pair")
	}

	authCtx := authContextFromAccessToken(t, jwt, resp.Authorization.AccessToken)
	overview, err := usersSvc.GetOverview(authCtx)
	if err != nil {
		t.Fatalf("get overview: %v", err)
	}
	if overview.Email != "alice@example.com" {
		t.Fatalf("expected overview email alice@example.com, got %q", overview.Email)
	}
}

func TestUsersService_DeleteAccountRemovesTodos(t *testing.T) {
	jwt := newTestJWTMiddleware(t)
	usersRepo := users.NewInMemoryUsersRepository()
	todosRepo := todos.NewInMemoryTodosRepository()
	usersSvc := NewUsersService(usersRepo, todosRepo, jwt)
	usersSvc.Register(context.Background(), &domain.UserDTO{Name: "Bob", Email: "bob@example.com"})

	registered, err := usersRepo.GetOverview(context.Background(), "")
	if err == nil || registered != nil {
		t.Fatal("expected no user lookup by empty ID")
	}

	// register a user by explicitly creating the account through service for a stable token
	resp, err := usersSvc.Register(context.Background(), &domain.UserDTO{Name: "Charlie", Email: "charlie@example.com"})
	if err != nil {
		t.Fatalf("register user: %v", err)
	}

	authCtx := authContextFromAccessToken(t, jwt, resp.Authorization.AccessToken)
	todoSvc := NewTodosService(todosRepo, usersRepo)
	if _, err := todoSvc.CreateTodo(authCtx, &domain.CreateTodoDTO{Title: "Delete me"}); err != nil {
		t.Fatalf("create todo: %v", err)
	}

	if err := usersSvc.DeleteAccount(authCtx); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	if remaining, err := todosRepo.GetAll(context.Background(), resp.Overview.ID); err != nil {
		t.Fatalf("fetch remaining todos after delete: %v", err)
	} else if len(remaining) != 0 {
		t.Fatalf("expected no todos for deleted user, got %d", len(remaining))
	}

	if _, err := usersRepo.GetOverview(context.Background(), resp.Overview.ID); err == nil {
		t.Fatal("expected deleted user to be absent")
	}
}

func TestTodosService_CreateUpdateReadDelete(t *testing.T) {
	jwt := newTestJWTMiddleware(t)
	usersRepo := users.NewInMemoryUsersRepository()
	todosRepo := todos.NewInMemoryTodosRepository()
	usersSvc := NewUsersService(usersRepo, todosRepo, jwt)
	todoSvc := NewTodosService(todosRepo, usersRepo)

	resp, err := usersSvc.Register(context.Background(), &domain.UserDTO{Name: "Dana", Email: "dana@example.com"})
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	ctx := authContextFromAccessToken(t, jwt, resp.Authorization.AccessToken)

	created, err := todoSvc.CreateTodo(ctx, &domain.CreateTodoDTO{Title: "Write service tests"})
	if err != nil {
		t.Fatalf("create todo: %v", err)
	}
	if created.Title != "Write service tests" {
		t.Fatalf("expected created title Write service tests, got %q", created.Title)
	}

	updatedTitle := "Write ownership tests"
	updatedCompleted := true
	updated, err := todoSvc.UpdateTodo(ctx, created.ID, &domain.UpdateTodoDTO{Title: &updatedTitle, Completed: &updatedCompleted})
	if err != nil {
		t.Fatalf("update todo: %v", err)
	}
	if updated.Title != updatedTitle || !updated.Completed {
		t.Fatalf("expected updated todo values, got %#v", updated)
	}

	fetched, err := todoSvc.GetTodo(ctx, created.ID)
	if err != nil {
		t.Fatalf("get todo: %v", err)
	}
	if fetched.ID != created.ID {
		t.Fatalf("expected fetched id %q, got %q", created.ID, fetched.ID)
	}

	allTodos, err := todoSvc.GetTodos(ctx)
	if err != nil {
		t.Fatalf("list todos: %v", err)
	}
	if len(allTodos) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(allTodos))
	}

	if err := todoSvc.DeleteTodo(ctx, created.ID); err != nil {
		t.Fatalf("delete todo: %v", err)
	}

	if _, err := todoSvc.GetTodo(ctx, created.ID); err == nil {
		t.Fatal("expected deleted todo to be unavailable")
	}
}

func TestUsersService_RefreshTokenAndAuth(t *testing.T) {
	jwt := newTestJWTMiddleware(t)
	usersSvc := NewUsersService(users.NewInMemoryUsersRepository(), todos.NewInMemoryTodosRepository(), jwt)

	registered, err := usersSvc.Register(context.Background(), &domain.UserDTO{Name: "Frank", Email: "frank@example.com"})
	if err != nil {
		t.Fatalf("register user: %v", err)
	}

	refreshed, err := usersSvc.RefreshToken(context.Background(), &domain.RefreshRequest{RefreshToken: registered.Authorization.RefreshToken})
	if err != nil {
		t.Fatalf("refresh token: %v", err)
	}
	if refreshed == nil || refreshed.AccessToken == "" || refreshed.RefreshToken == "" {
		t.Fatal("expected refreshed token pair")
	}

	authCtx := authContextFromAccessToken(t, jwt, registered.Authorization.AccessToken)
	overview, err := usersSvc.Auth(authCtx)
	if err != nil {
		t.Fatalf("auth overview: %v", err)
	}
	if overview.ID != registered.Overview.ID {
		t.Fatalf("expected overview id %q, got %q", registered.Overview.ID, overview.ID)
	}
}

func TestTodosService_RequiresAuthorizationAndOwnsTodoAccess(t *testing.T) {
	jwt := newTestJWTMiddleware(t)
	usersRepo := users.NewInMemoryUsersRepository()
	todosRepo := todos.NewInMemoryTodosRepository()
	usersSvc := NewUsersService(usersRepo, todosRepo, jwt)
	todoSvc := NewTodosService(todosRepo, usersRepo)

	firstUser, err := usersSvc.Register(context.Background(), &domain.UserDTO{Name: "Gina", Email: "gina@example.com"})
	if err != nil {
		t.Fatalf("register first user: %v", err)
	}
	secondUser, err := usersSvc.Register(context.Background(), &domain.UserDTO{Name: "Henry", Email: "henry@example.com"})
	if err != nil {
		t.Fatalf("register second user: %v", err)
	}

	firstCtx := authContextFromAccessToken(t, jwt, firstUser.Authorization.AccessToken)
	secondCtx := authContextFromAccessToken(t, jwt, secondUser.Authorization.AccessToken)

	created, err := todoSvc.CreateTodo(firstCtx, &domain.CreateTodoDTO{Title: "Owned by Gina"})
	if err != nil {
		t.Fatalf("create todo for first user: %v", err)
	}

	if _, err := todoSvc.GetTodo(secondCtx, created.ID); err == nil {
		t.Fatal("expected cross-user todo access to be forbidden")
	}

	if _, err := todoSvc.GetTodo(context.Background(), created.ID); err == nil {
		t.Fatal("expected missing authorization context to be rejected")
	}
}
