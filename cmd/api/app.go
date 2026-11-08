package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/app"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ratelimit"
)

// api is the wired-up HTTP server: the composition root is the only place
// that knows every concrete adapter.
type api struct {
	cfg     *config.Config
	handler http.Handler
	// worker runs background jobs inside the API process (JOBS_RUN_IN_API,
	// or DB_TYPE=memory); nil when cmd/worker runs them.
	worker *app.Worker
	close  func()
}

func newApp(ctx context.Context, cfg *config.Config) (*api, error) {
	store, err := app.NewStorage(ctx, cfg)
	if err != nil {
		return nil, err
	}

	signIn, err := app.NewSignIn(ctx, cfg)
	if err != nil {
		store.Close()
		return nil, err
	}

	pusher, err := app.NewPusher(ctx, cfg)
	if err != nil {
		store.Close()
		return nil, err
	}

	modules := app.Wire(store, signIn, app.Push{Pusher: pusher}, app.NewMailers(cfg), cfg)
	var worker *app.Worker
	if cfg.Jobs.RunInAPI || cfg.DB.DBType == "memory" {
		if worker, err = app.NewWorker(store, modules, cfg); err != nil {
			store.Close()
			return nil, err
		}
	} else {
		slog.Info("background jobs run in cmd/worker (set JOBS_RUN_IN_API=true to run them here)")
	}
	return &api{
		cfg:     cfg,
		handler: newRouter(cfg, modules),
		worker:  worker,
		close:   store.Close,
	}, nil
}

func newRouter(cfg *config.Config, m app.Modules) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	authenticate := auth.Middleware(m.Identity.Authenticator())
	limiter := ratelimit.New(cfg.Auth.RateLimitPerMinute, cfg.Auth.RateLimitBurst)

	// Everything but identity runs as an access.Actor (the caller plus their
	// team roles), and may require a verified email.
	verified := authenticate
	if cfg.Auth.RequireVerifiedEmail {
		verified = func(h http.Handler) http.Handler { return authenticate(auth.RequireVerifiedEmail(h)) }
	}
	resolve := access.Middleware(m.Teams.Resolver())
	protect := func(h http.Handler) http.Handler { return verified(resolve(h)) }

	// Creating todos has one per-user budget, over REST and MCP alike.
	todoCreates := ratelimit.New(cfg.Limits.TodoCreatesPerMinute, cfg.Limits.TodoCreateBurst)

	m.Identity.RegisterRoutes(mux, authenticate, limiter.Middleware)
	m.Todos.RegisterRoutes(mux, protect, todoCreates.MiddlewareBy(principalKey))
	m.Teams.RegisterRoutes(mux, protect, limiter.Middleware)
	m.Audit.RegisterRoutes(mux, protect)
	m.Notifications.RegisterRoutes(mux, protect)
	jobs.RegisterOperations(mux, m.Jobs, authenticate)

	// The MCP endpoint and the OAuth server MCP clients sign in through need
	// the sign-in page's Firebase web configuration.
	if cfg.Firebase.WebAPIKey != "" {
		registrations := ratelimit.New(oauthRegistrationsPerMinute, oauthRegistrationBurst)
		m.OAuth.RegisterRoutes(mux, authenticate, limiter.Middleware, registrations.Middleware)
		mux.Handle(app.MCPPath, newMCPHandler(cfg, m, resolve, todoCreates.Check))
	} else {
		slog.Warn("FIREBASE_WEB_API_KEY not set: the MCP endpoint and its OAuth server are disabled")
	}

	return httpx.Chain(httpx.JSONErrors(mux),
		httpx.Recovery,
		httpx.RequestID,
		httpx.ClientInfo,
		httpx.AccessLog,
		func(h http.Handler) http.Handler { return httpx.LimitBody(httpx.DefaultMaxBodyBytes, h) },
	)
}

// shutdownGrace is how long requests in flight, then notifications being
// sent, get to finish after SIGTERM.
const shutdownGrace = 5 * time.Second

func (a *api) run(ctx context.Context) error {
	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", a.cfg.App.Port),
		Handler:           a.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    1 << 16,
	}

	// ListenAndServe returns as soon as Shutdown starts; run waits for
	// stopped to know requests and notifications are done before the caller
	// closes the database.
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown failed", "error", err)
		}
	}()
	if a.worker != nil {
		// Stops with the server (ctx), finishing the jobs it is running.
		workerDone := make(chan struct{})
		go func() {
			defer close(workerDone)
			a.worker.Run(ctx)
		}()
		defer func() { <-workerDone }()
	}

	slog.Info("server started", "addr", server.Addr, "db", a.cfg.DB.DBType)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-stopped
	return nil
}

// Dynamic client registration comes from MCP hosts' servers, and every
// Claude user shares Anthropic's few addresses: a per-IP budget generous
// enough for that, still bounding what one address can store.
const (
	oauthRegistrationsPerMinute = 600
	oauthRegistrationBurst      = 100
)

// principalKey keys a per-user limit by the signed-in user (it runs after
// authentication); requests without one share a bucket.
func principalKey(r *http.Request) string {
	p, _ := auth.PrincipalFrom(r.Context())
	return p.UserID
}
