package app

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"

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
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/mail"
)

type Modules struct {
	// Jobs is the job store, for the operations endpoint and tool.
	Jobs          jobs.Store
	OAuth         *oauth.Module
	Audit         *audit.Module
	Notifications *notifications.Module
	Todos         *todos.Module
	Teams         *teams.Module
	Identity      *identity.Module
}

// mailers are the modules' email adapters.
type Mailers struct {
	Teams teamsapp.Mailer
}

// signIn are the adapters identity authenticates with: Firebase and WebAuthn.
type SignIn struct {
	IDTokens application.IDTokenVerifier
	Firebase application.FirebaseAdmin // nil without a service account
	Passkeys application.RelyingParty
	// clientMetadata fetches MCP clients' Client ID Metadata Documents.
	ClientMetadata oauth.ClientMetadataFetcher
}

// NewSignIn connects to the Firebase project (FIREBASE_PROJECT_ID) and sets
// up passkeys. Without FIREBASE_CREDENTIALS_FILE the API only verifies ID
// tokens: passkey sign-in is off and deleting an account leaves its Firebase
// user (the client deletes it).
func NewSignIn(ctx context.Context, cfg *config.Config) (SignIn, error) {
	client, err := firebase.New(ctx, cfg.Firebase.ProjectID, cfg.Firebase.CredentialsFile)
	if err != nil {
		return SignIn{}, fmt.Errorf("initialize Firebase: %w", err)
	}
	rp, err := NewRelyingParty(cfg)
	if err != nil {
		return SignIn{}, fmt.Errorf("initialize passkeys: %w", err)
	}
	s := SignIn{IDTokens: client, Passkeys: rp, ClientMetadata: cimd.New()}
	if cfg.Firebase.CredentialsFile != "" {
		s.Firebase = client
	} else {
		slog.Warn("FIREBASE_CREDENTIALS_FILE not set: passkey sign-in is disabled and deleted accounts keep their Firebase user")
	}
	return s, nil
}

// Push is how notifications are delivered: the FCM pusher (nil: off).
type Push struct {
	Pusher notifications.Pusher
}

// Wire builds the modules in dependency order: audit is a leaf, todos
// records into it, teams deletes team data in todos, identity reaches all of
// them on account deletion. Team memberships flow to todos and audit through
// platform/access (teams.Resolver, teams.NewDirectory), not through imports.
func Wire(store *Storage, signIn SignIn, push Push, mailer Mailers, cfg *config.Config) Modules {
	// Jobs are enqueued inside the callers' units of work (the outbox) and
	// run by the worker.
	queue := jobs.NewQueue(store.Jobs, nil)
	auditModule := audit.New(audit.Deps{Events: store.Events})
	// notifications is built before teams (teams → todos → notifications),
	// so it reads team rosters straight from the team stores.
	notificationsModule := notifications.New(notifications.Deps{
		Devices:   store.Devices,
		Directory: teams.NewDirectory(store.Teams, store.Members),
		Pusher:    push.Pusher,
		Jobs:      queue,
	})
	todosModule := todos.New(todos.Deps{
		Todos:       store.Todos,
		Projects:    store.Projects,
		Collections: store.Collections,
		Audit:       todosaudit.New(auditModule),
		Notifier:    todosnotifications.New(notificationsModule),
		Tx:          store.Tx,
	})
	teamsModule := teams.New(teams.Deps{
		Teams:       store.Teams,
		Members:     store.Members,
		Invitations: store.Invitations,
		TeamData:    teamstodos.New(todosModule),
		Audit:       teamsaudit.New(auditModule),
		Mailer:      mailer.Teams,
		Tx:          store.Tx,
		Config:      teams.DefaultConfig(cfg.App.BaseURL),
		Jobs:        queue,
	})
	// oauth is built before identity (identity revokes its connections), so
	// it verifies Firebase ID tokens straight from the account store.
	issuer := strings.TrimSuffix(cfg.App.BaseURL, "/")
	oauthModule := oauth.New(oauth.Deps{
		Clients:  store.OAuth.Clients,
		Codes:    store.OAuth.Codes,
		Grants:   store.OAuth.Grants,
		Metadata: signIn.ClientMetadata,
		Accounts: identity.NewAuthenticator(store.Users, signIn.IDTokens),
		Tx:       store.Tx,
		Config: oauth.Config{
			Issuer:   issuer,
			Resource: issuer + MCPPath,
			SignIn: oauth.SignIn{
				APIKey:     cfg.Firebase.WebAPIKey,
				AuthDomain: cfg.Firebase.AuthDomain,
				ProjectID:  cfg.Firebase.ProjectID,
			},
		},
	})
	identityModule := identity.New(identity.Deps{
		Users:        store.Users,
		Passkeys:     store.Passkeys,
		Challenges:   store.Challenges,
		RelyingParty: signIn.Passkeys,
		IDTokens:     signIn.IDTokens,
		Firebase:     signIn.Firebase,
		Todos:        identitytodos.New(todosModule),
		Teams:        identityteams.New(teamsModule),
		Audit:        identityaudit.New(auditModule),
		Devices:      identitynotifications.New(notificationsModule),
		Connections:  identityoauth.New(oauthModule),
		Tx:           store.Tx,
		Config:       identity.DefaultConfig(),
		Jobs:         queue,
	})
	return Modules{
		Jobs:          store.Jobs,
		OAuth:         oauthModule,
		Audit:         auditModule,
		Notifications: notificationsModule,
		Todos:         todosModule,
		Teams:         teamsModule,
		Identity:      identityModule,
	}
}

// NewPusher sends push notifications with FCM when a Firebase service
// account is configured; without one devices are still registered but
// nothing is sent.
func NewPusher(ctx context.Context, cfg *config.Config) (notifications.Pusher, error) {
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

// NewRelyingParty configures passkeys for PASSKEY_RP_ID and PASSKEY_ORIGINS,
// each defaulting to the host and origin of APP_BASE_URL.
func NewRelyingParty(cfg *config.Config) (*passkeys.RelyingParty, error) {
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

func NewMailers(cfg *config.Config) Mailers {
	var sender mail.Sender = mail.LogSender{}
	if cfg.SMTP.Host == "" {
		slog.Warn("SMTP_HOST not set: invitation emails are logged, not sent")
	} else {
		sender = mail.SMTP{
			Host: cfg.SMTP.Host, Port: cfg.SMTP.Port,
			Username: cfg.SMTP.Username, Password: cfg.SMTP.Password, From: cfg.SMTP.From,
		}
	}
	return Mailers{Teams: teamsmail.Mailer{Sender: sender}}
}

// MCPPath is the MCP endpoint, relative to APP_BASE_URL: the audience of the
// OAuth tokens MCP clients get.
const MCPPath = "/mcp"
