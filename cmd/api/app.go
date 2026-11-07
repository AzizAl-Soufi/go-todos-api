package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity"
	identityaudit "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/auditgateway"
	identitymail "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/mail"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/passkeys"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/security"
	identityteams "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/teamsgateway"
	identitytodos "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/todosgateway"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams"
	teamsaudit "github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/auditgateway"
	teamsmail "github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/mail"
	teamstodos "github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/todosgateway"
	teamsapp "github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos"
	todosaudit "github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/auditgateway"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/mail"
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

	rp, err := newRelyingParty(cfg)
	if err != nil {
		store.close()
		return nil, fmt.Errorf("initialize passkeys: %w", err)
	}

	modules := wireModules(store, security.NewBcryptHasher(security.DefaultBcryptCost), tokens, rp, newMailers(cfg), cfg)
	return &app{cfg: cfg, handler: newRouter(cfg, modules), close: store.close}, nil
}

type modules struct {
	audit    *audit.Module
	todos    *todos.Module
	teams    *teams.Module
	identity *identity.Module
}

// mailers are the modules' email adapters.
type mailers struct {
	identity application.Mailer
	teams    teamsapp.Mailer
}

// wireModules builds the modules in dependency order: audit is a leaf, todos
// records into it, teams deletes team data in todos, identity reaches all of
// them on account deletion. Team memberships flow to todos and audit through
// platform/access (teams.Resolver), not through imports.
func wireModules(store *storage, hasher application.PasswordHasher, tokens application.AccessTokens, rp application.RelyingParty, mailer mailers, cfg *config.Config) modules {
	auditModule := audit.New(audit.Deps{Events: store.events})
	todosModule := todos.New(todos.Deps{
		Todos:       store.todos,
		Projects:    store.projects,
		Collections: store.collections,
		Audit:       todosaudit.New(auditModule),
		Tx:          store.tx,
	})
	teamsModule := teams.New(teams.Deps{
		Teams:       store.teams,
		Members:     store.members,
		Invitations: store.invitations,
		TeamData:    teamstodos.New(todosModule),
		Audit:       teamsaudit.New(auditModule),
		Mailer:      mailer.teams,
		Tx:          store.tx,
		Config:      teams.DefaultConfig(cfg.App.BaseURL),
	})
	identityModule := identity.New(identity.Deps{
		Users:          store.users,
		Sessions:       store.sessions,
		Verifications:  store.verifications,
		PasswordResets: store.passwordResets,
		Passkeys:       store.passkeys,
		Challenges:     store.challenges,
		RelyingParty:   rp,
		Hasher:         hasher,
		Tokens:         tokens,
		Mailer:         mailer.identity,
		Todos:          identitytodos.New(todosModule),
		Teams:          identityteams.New(teamsModule),
		Audit:          identityaudit.New(auditModule),
		Tx:             store.tx,
		Config:         identityConfig(cfg),
	})
	return modules{audit: auditModule, todos: todosModule, teams: teamsModule, identity: identityModule}
}

func identityConfig(cfg *config.Config) identity.Config {
	c := identity.DefaultConfig(cfg.App.BaseURL)
	if cfg.Auth.PasswordResetURL != "" {
		c.PasswordResetURL = cfg.Auth.PasswordResetURL
	}
	return c
}

// newRelyingParty configures passkeys for PASSKEY_RP_ID and PASSKEY_ORIGINS,
// each defaulting to the host and origin of APP_BASE_URL.
func newRelyingParty(cfg *config.Config) (*passkeys.RelyingParty, error) {
	pc := passkeys.Config{RPID: cfg.Passkey.RPID, RPName: cfg.Passkey.RPName}
	for o := range strings.SplitSeq(cfg.Passkey.Origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			pc.Origins = append(pc.Origins, o)
		}
	}
	if pc.RPID == "" || len(pc.Origins) == 0 {
		def, err := passkeys.ConfigFromBaseURL(cfg.App.BaseURL, pc.RPName)
		if err != nil {
			return nil, err
		}
		pc.RPID = cmp.Or(pc.RPID, def.RPID)
		if len(pc.Origins) == 0 {
			pc.Origins = def.Origins
		}
	}
	pc.RPName = cmp.Or(pc.RPName, "Todos")
	pc.Timeout = identityConfig(cfg).PasskeyChallengeTTL
	return passkeys.New(pc)
}

func newMailers(cfg *config.Config) mailers {
	var sender mail.Sender = mail.LogSender{}
	if cfg.SMTP.Host == "" {
		slog.Warn("SMTP_HOST not set: verification, password reset and invitation emails are logged, not sent")
	} else {
		sender = mail.SMTP{
			Host: cfg.SMTP.Host, Port: cfg.SMTP.Port,
			Username: cfg.SMTP.Username, Password: cfg.SMTP.Password, From: cfg.SMTP.From,
		}
	}
	return mailers{identity: identitymail.Mailer{Sender: sender}, teams: teamsmail.Mailer{Sender: sender}}
}

func newRouter(cfg *config.Config, m modules) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})

	authenticate := auth.Middleware(m.identity.Authenticator())
	limiter := ratelimit.New(cfg.Auth.RateLimitPerMinute, cfg.Auth.RateLimitBurst)

	// Everything but identity runs as an access.Actor (the caller plus their
	// team roles), and may require a verified email.
	verified := authenticate
	if cfg.Auth.RequireVerifiedEmail {
		verified = func(h http.Handler) http.Handler { return authenticate(auth.RequireVerifiedEmail(h)) }
	}
	resolve := access.Middleware(m.teams.Resolver())
	protect := func(h http.Handler) http.Handler { return verified(resolve(h)) }

	m.identity.RegisterRoutes(mux, authenticate, limiter.Middleware)
	m.todos.RegisterRoutes(mux, protect)
	m.teams.RegisterRoutes(mux, protect, limiter.Middleware)
	m.audit.RegisterRoutes(mux, protect)

	return httpx.Chain(httpx.JSONErrors(mux),
		httpx.Recovery,
		httpx.RequestID,
		httpx.ClientInfo,
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
