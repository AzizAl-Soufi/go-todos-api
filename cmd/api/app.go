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
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/firebase"
	identitynotifications "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/notificationsgateway"
	identityoauth "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/oauthgateway"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/passkeys"
	identityteams "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/teamsgateway"
	identitytodos "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/todosgateway"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/adapters/fcm"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/cimd"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams"
	teamsaudit "github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/auditgateway"
	teamsmail "github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/mail"
	teamstodos "github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/todosgateway"
	teamsapp "github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos"
	todosaudit "github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/auditgateway"
	todosnotifications "github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/notificationsgateway"
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
	// drain waits for background work (notifications) after the server has
	// stopped, before close releases the database.
	drain func(context.Context) error
	close func()
}

func newApp(ctx context.Context, cfg *config.Config) (*app, error) {
	store, err := newStorage(ctx, cfg)
	if err != nil {
		return nil, err
	}

	signIn, err := newSignIn(ctx, cfg)
	if err != nil {
		store.close()
		return nil, err
	}

	pusher, err := newPusher(ctx, cfg)
	if err != nil {
		store.close()
		return nil, err
	}

	modules := wireModules(store, signIn, push{pusher: pusher}, newMailers(cfg), cfg)
	return &app{
		cfg:     cfg,
		handler: newRouter(cfg, modules),
		drain:   modules.notifications.Close,
		close:   store.close,
	}, nil
}

type modules struct {
	oauth         *oauth.Module
	audit         *audit.Module
	notifications *notifications.Module
	todos         *todos.Module
	teams         *teams.Module
	identity      *identity.Module
}

// mailers are the modules' email adapters.
type mailers struct {
	teams teamsapp.Mailer
}

// signIn are the adapters identity authenticates with: Firebase and WebAuthn.
type signIn struct {
	idTokens application.IDTokenVerifier
	firebase application.FirebaseAdmin // nil without a service account
	passkeys application.RelyingParty
	// clientMetadata fetches MCP clients' Client ID Metadata Documents.
	clientMetadata oauth.ClientMetadataFetcher
}

// newSignIn connects to the Firebase project (FIREBASE_PROJECT_ID) and sets
// up passkeys. Without FIREBASE_CREDENTIALS_FILE the API only verifies ID
// tokens: passkey sign-in is off and deleting an account leaves its Firebase
// user (the client deletes it).
func newSignIn(ctx context.Context, cfg *config.Config) (signIn, error) {
	client, err := firebase.New(ctx, cfg.Firebase.ProjectID, cfg.Firebase.CredentialsFile)
	if err != nil {
		return signIn{}, fmt.Errorf("initialize Firebase: %w", err)
	}
	rp, err := newRelyingParty(cfg)
	if err != nil {
		return signIn{}, fmt.Errorf("initialize passkeys: %w", err)
	}
	s := signIn{idTokens: client, passkeys: rp, clientMetadata: cimd.New()}
	if cfg.Firebase.CredentialsFile != "" {
		s.firebase = client
	} else {
		slog.Warn("FIREBASE_CREDENTIALS_FILE not set: passkey sign-in is disabled and deleted accounts keep their Firebase user")
	}
	return s, nil
}

// push is how notifications are delivered: the FCM pusher (nil: off) and
// how they run after the request (nil: in the background, drained on
// shutdown; tests run them in place).
type push struct {
	pusher   notifications.Pusher
	dispatch func(func())
}

// wireModules builds the modules in dependency order: audit is a leaf, todos
// records into it, teams deletes team data in todos, identity reaches all of
// them on account deletion. Team memberships flow to todos and audit through
// platform/access (teams.Resolver, teams.NewDirectory), not through imports.
func wireModules(store *storage, signIn signIn, push push, mailer mailers, cfg *config.Config) modules {
	auditModule := audit.New(audit.Deps{Events: store.events})
	// notifications is built before teams (teams → todos → notifications),
	// so it reads team rosters straight from the team stores.
	notificationsModule := notifications.New(notifications.Deps{
		Devices:   store.devices,
		Directory: teams.NewDirectory(store.teams, store.members),
		Pusher:    push.pusher,
		Dispatch:  push.dispatch,
	})
	todosModule := todos.New(todos.Deps{
		Todos:       store.todos,
		Projects:    store.projects,
		Collections: store.collections,
		Audit:       todosaudit.New(auditModule),
		Notifier:    todosnotifications.New(notificationsModule),
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
	// oauth is built before identity (identity revokes its connections), so
	// it verifies Firebase ID tokens straight from the account store.
	issuer := strings.TrimSuffix(cfg.App.BaseURL, "/")
	oauthModule := oauth.New(oauth.Deps{
		Clients:  store.oauth.clients,
		Codes:    store.oauth.codes,
		Grants:   store.oauth.grants,
		Metadata: signIn.clientMetadata,
		Accounts: identity.NewAuthenticator(store.users, signIn.idTokens),
		Tx:       store.tx,
		Config: oauth.Config{
			Issuer:   issuer,
			Resource: issuer + mcpPath,
			SignIn: oauth.SignIn{
				APIKey:     cfg.Firebase.WebAPIKey,
				AuthDomain: cfg.Firebase.AuthDomain,
				ProjectID:  cfg.Firebase.ProjectID,
			},
		},
	})
	identityModule := identity.New(identity.Deps{
		Users:        store.users,
		Passkeys:     store.passkeys,
		Challenges:   store.challenges,
		RelyingParty: signIn.passkeys,
		IDTokens:     signIn.idTokens,
		Firebase:     signIn.firebase,
		Todos:        identitytodos.New(todosModule),
		Teams:        identityteams.New(teamsModule),
		Audit:        identityaudit.New(auditModule),
		Devices:      identitynotifications.New(notificationsModule),
		Connections:  identityoauth.New(oauthModule),
		Tx:           store.tx,
		Config:       identity.DefaultConfig(),
	})
	return modules{
		oauth:         oauthModule,
		audit:         auditModule,
		notifications: notificationsModule,
		todos:         todosModule,
		teams:         teamsModule,
		identity:      identityModule,
	}
}

// newPusher sends push notifications with FCM when a Firebase service
// account is configured; without one devices are still registered but
// nothing is sent.
func newPusher(ctx context.Context, cfg *config.Config) (notifications.Pusher, error) {
	if cfg.Firebase.CredentialsFile == "" {
		slog.Warn("FIREBASE_CREDENTIALS_FILE not set: push notifications are disabled")
		return nil, nil
	}
	p, err := fcm.New(ctx, cfg.Firebase.ProjectID, cfg.Firebase.CredentialsFile)
	if err != nil {
		return nil, fmt.Errorf("initialize push notifications: %w", err)
	}
	return p, nil
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
	pc.Timeout = identity.DefaultConfig().PasskeyChallengeTTL
	return passkeys.New(pc)
}

func newMailers(cfg *config.Config) mailers {
	var sender mail.Sender = mail.LogSender{}
	if cfg.SMTP.Host == "" {
		slog.Warn("SMTP_HOST not set: invitation emails are logged, not sent")
	} else {
		sender = mail.SMTP{
			Host: cfg.SMTP.Host, Port: cfg.SMTP.Port,
			Username: cfg.SMTP.Username, Password: cfg.SMTP.Password, From: cfg.SMTP.From,
		}
	}
	return mailers{teams: teamsmail.Mailer{Sender: sender}}
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

	// Creating todos has one per-user budget, over REST and MCP alike.
	todoCreates := ratelimit.New(cfg.Limits.TodoCreatesPerMinute, cfg.Limits.TodoCreateBurst)

	m.identity.RegisterRoutes(mux, authenticate, limiter.Middleware)
	m.todos.RegisterRoutes(mux, protect, todoCreates.MiddlewareBy(principalKey))
	m.teams.RegisterRoutes(mux, protect, limiter.Middleware)
	m.audit.RegisterRoutes(mux, protect)
	m.notifications.RegisterRoutes(mux, protect)

	// The MCP endpoint and the OAuth server MCP clients sign in through need
	// the sign-in page's Firebase web configuration.
	if cfg.Firebase.WebAPIKey != "" {
		registrations := ratelimit.New(oauthRegistrationsPerMinute, oauthRegistrationBurst)
		m.oauth.RegisterRoutes(mux, authenticate, limiter.Middleware, registrations.Middleware)
		mux.Handle(mcpPath, newMCPHandler(cfg, m, resolve, todoCreates.Check))
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
		if err := a.drain(shutdownCtx); err != nil {
			slog.Error("notifications still being sent at shutdown", "error", err)
		}
	}()

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
