package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/mail"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/security"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/todosgateway"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ratelimit"
)

// app is the wired-up application: the composition root is the only place
// that knows every concrete adapter.
type app struct {
	cfg     *config.Config
	handler http.Handler
	close   func()
}

func newApp(ctx context.Context, cfg *config.Config) (*app, error) {
	store, err := newStorage(ctx, cfg)
	if err != nil {
		return nil, err
	}

	tokens, err := security.NewJWTFromFiles(cfg.JWT.PrivateKeyPath, cfg.JWT.PublicKeyPath, security.DefaultAccessTokenTTL)
	if err != nil {
		store.close()
		return nil, fmt.Errorf("initialize JWT: %w", err)
	}

	modules := wireModules(store, security.NewBcryptHasher(security.DefaultBcryptCost), tokens, newMailer(cfg), cfg)
	return &app{cfg: cfg, handler: newRouter(cfg, modules), close: store.close}, nil
}

type modules struct {
	identity *identity.Module
	todos    *todos.Module
}

func wireModules(store *storage, hasher application.PasswordHasher, tokens application.AccessTokens, mailer application.Mailer, cfg *config.Config) modules {
	todosModule := todos.New(todos.Deps{
		Todos:       store.todos,
		Projects:    store.projects,
		Collections: store.collections,
		Tx:          store.tx,
	})
	identityModule := identity.New(identity.Deps{
		Users:          store.users,
		Sessions:       store.sessions,
		Verifications:  store.verifications,
		PasswordResets: store.passwordResets,
		Hasher:         hasher,
		Tokens:         tokens,
		Mailer:         mailer,
		Todos:          todosgateway.New(todosModule),
		Tx:             store.tx,
		Config:         identityConfig(cfg),
	})
	return modules{identity: identityModule, todos: todosModule}
}

func identityConfig(cfg *config.Config) identity.Config {
	c := identity.DefaultConfig(cfg.App.BaseURL)
	if cfg.Auth.PasswordResetURL != "" {
		c.PasswordResetURL = cfg.Auth.PasswordResetURL
	}
	return c
}

func newMailer(cfg *config.Config) application.Mailer {
	if cfg.SMTP.Host == "" {
		slog.Warn("SMTP_HOST not set: verification and password reset emails are logged, not sent")
		return mail.LogMailer{}
	}
	return mail.SMTPMailer{
		Host: cfg.SMTP.Host, Port: cfg.SMTP.Port,
		Username: cfg.SMTP.Username, Password: cfg.SMTP.Password, From: cfg.SMTP.From,
	}
}

func newRouter(cfg *config.Config, m modules) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	authenticate := auth.Middleware(m.identity.Authenticator())
	limiter := ratelimit.New(cfg.Auth.RateLimitPerMinute, cfg.Auth.RateLimitBurst)

	protectTodos := authenticate
	if cfg.Auth.RequireVerifiedEmail {
		protectTodos = func(h http.Handler) http.Handler { return authenticate(auth.RequireVerifiedEmail(h)) }
	}

	m.identity.RegisterRoutes(mux, authenticate, limiter.Middleware)
	m.todos.RegisterRoutes(mux, protectTodos)

	return httpx.Chain(httpx.JSONErrors(mux),
		httpx.Recovery,
		httpx.RequestID,
		httpx.AccessLog,
		func(h http.Handler) http.Handler { return httpx.LimitBody(httpx.DefaultMaxBodyBytes, h) },
	)
}

func (a *app) run(ctx context.Context) error {
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", a.cfg.App.Port),
		Handler:           a.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    1 << 16,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown failed", "error", err)
		}
	}()

	slog.Info("server started", "addr", server.Addr, "db", a.cfg.DB.DBType)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
