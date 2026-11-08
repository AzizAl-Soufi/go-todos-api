package app

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
	jobsmem "github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/memstore"
	jobsmongo "github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/mongostore"
	jobspg "github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/pgstore"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit"
	auditmem "github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/memstore"
	auditmongo "github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/mongostore"
	auditpg "github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/pgstore"
	identitymem "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/memstore"
	identitymongo "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/mongostore"
	identitypg "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications"
	notificationsmem "github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/adapters/persistence/memstore"
	notificationsmongo "github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/adapters/persistence/mongostore"
	notificationspg "github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth"
	oauthmem "github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/memstore"
	oauthmongo "github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/mongostore"
	oauthpg "github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams"
	teamsmem "github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/persistence/memstore"
	teamsmongo "github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/persistence/mongostore"
	teamspg "github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos"
	todosmem "github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/memstore"
	todosmongo "github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/mongostore"
	todospg "github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/config"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	mongomigrations "github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
	_ "github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations/versions"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

// OAuthStores are the oauth module's stores.
type OAuthStores struct {
	Clients oauth.ClientStore
	Codes   oauth.CodeStore
	Grants  oauth.GrantStore
}

// storage bundles every persistence adapter for one backend.
type Storage struct {
	Todos       todos.TodoStore
	Projects    todos.ProjectStore
	Collections todos.CollectionStore
	Users       application.UserRepository
	Passkeys    application.PasskeyRepository
	Challenges  application.PasskeyChallengeRepository
	Teams       teams.TeamStore
	Members     teams.MemberStore
	Invitations teams.InvitationStore
	Events      audit.EventStore
	Devices     notifications.DeviceStore
	Jobs        jobs.Store
	OAuth       OAuthStores
	Tx          tx.Runner
	Close       func()
}

func NewStorage(ctx context.Context, cfg *config.Config) (*Storage, error) {
	switch cfg.DB.DBType {
	case "memory":
		return NewMemoryStorage(), nil
	case "mongo":
		return newMongoStorage(ctx, cfg)
	case "postgres":
		return newPostgresStorage(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported DB_TYPE %q (want memory, mongo or postgres)", cfg.DB.DBType)
	}
}

func NewMemoryStorage() *Storage {
	oauthMem := oauthmem.New()
	// Linked, so due-date alerts skip todos of archived projects.
	todosMem, projectsMem := todosmem.NewTodos(), todosmem.NewProjects()
	todosMem.LinkProjects(projectsMem)
	return &Storage{
		Todos:       todosMem,
		Projects:    projectsMem,
		Collections: todosmem.NewCollections(),
		Users:       identitymem.NewUsers(),
		Passkeys:    identitymem.NewPasskeys(),
		Challenges:  identitymem.NewPasskeyChallenges(),
		Teams:       teamsmem.NewTeams(),
		Members:     teamsmem.NewMembers(),
		Invitations: teamsmem.NewInvitations(),
		Events:      auditmem.NewEvents(),
		Devices:     notificationsmem.NewDevices(),
		Jobs:        jobsmem.New(),
		OAuth: OAuthStores{
			// Linked, so the cleanup can tell which clients are in use.
			Clients: oauthMem.Clients,
			Codes:   oauthMem.Codes,
			Grants:  oauthMem.Grants,
		},
		Tx:    tx.None{},
		Close: func() {},
	}
}

func newMongoStorage(ctx context.Context, cfg *config.Config) (*Storage, error) {
	if cfg.DB.DatabaseURI == "" || cfg.DB.DatabaseName == "" {
		return nil, fmt.Errorf("DATABASE_URI and DATABASE_NAME are required for DB_TYPE=mongo")
	}
	client, err := mongodb.New(ctx, &cfg.DB)
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping MongoDB: %w", err)
	}
	if cfg.App.MigrateOnStart {
		if err := migrateMongo(client, cfg.DB.MongoMigrationsConfig); err != nil { //nolint:contextcheck // the Mongo migration runner takes no context
			return nil, err
		}
	}

	store := MongoStorage(ctx, client)
	store.Close = func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := client.Close(ctx); err != nil {
			slog.Error("closing MongoDB connection", "error", err)
		}
	}
	return store, nil
}

// MongoStorage builds the adapters on a connected, migrated database.
func MongoStorage(ctx context.Context, client mongodb.MongoDBClient) *Storage {
	db := client.Database()
	return &Storage{
		Todos:       todosmongo.NewTodos(db),
		Projects:    todosmongo.NewProjects(db),
		Collections: todosmongo.NewCollections(db),
		Users:       identitymongo.NewUsers(db),
		Passkeys:    identitymongo.NewPasskeys(db),
		Challenges:  identitymongo.NewPasskeyChallenges(db),
		Teams:       teamsmongo.NewTeams(db),
		Members:     teamsmongo.NewMembers(db),
		Invitations: teamsmongo.NewInvitations(db),
		Events:      auditmongo.NewEvents(db),
		Devices:     notificationsmongo.NewDevices(db),
		Jobs:        jobsmongo.New(db),
		OAuth: OAuthStores{
			Clients: oauthmongo.NewClients(db),
			Codes:   oauthmongo.NewCodes(db),
			Grants:  oauthmongo.NewGrants(db),
		},
		Tx:    mongodb.NewTxRunner(ctx, client),
		Close: func() {},
	}
}

func newPostgresStorage(ctx context.Context, cfg *config.Config) (*Storage, error) {
	if cfg.DB.DatabaseURI == "" {
		return nil, fmt.Errorf("DATABASE_URI is required for DB_TYPE=postgres")
	}
	client, err := postgres.New(ctx, cfg.DB.DatabaseURI)
	if err != nil {
		return nil, err
	}
	pool := client.Pool()
	if cfg.App.MigrateOnStart {
		if err := postgres.Migrate(ctx, pool); err != nil {
			_ = client.Close() // the migration error is the one to report
			return nil, err
		}
	}

	store := PostgresStorage(pool)
	store.Close = func() {
		if err := client.Close(); err != nil {
			slog.Error("closing PostgreSQL connection", "error", err)
		}
	}
	return store, nil
}

// PostgresStorage builds the adapters on a connected, migrated pool.
func PostgresStorage(pool *pgxpool.Pool) *Storage {
	return &Storage{
		Todos:       todospg.NewTodos(pool),
		Projects:    todospg.NewProjects(pool),
		Collections: todospg.NewCollections(pool),
		Users:       identitypg.NewUsers(pool),
		Passkeys:    identitypg.NewPasskeys(pool),
		Challenges:  identitypg.NewPasskeyChallenges(pool),
		Teams:       teamspg.NewTeams(pool),
		Members:     teamspg.NewMembers(pool),
		Invitations: teamspg.NewInvitations(pool),
		Events:      auditpg.NewEvents(pool),
		Devices:     notificationspg.NewDevices(pool),
		Jobs:        jobspg.New(pool),
		OAuth: OAuthStores{
			Clients: oauthpg.NewClients(pool),
			Codes:   oauthpg.NewCodes(pool),
			Grants:  oauthpg.NewGrants(pool),
		},
		Tx:    postgres.NewTxRunner(pool),
		Close: func() {},
	}
}

func migrateMongo(client mongodb.MongoDBClient, configPath string) error {
	migCfg, err := mongomigrations.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load MongoDB migration config %s: %w", configPath, err)
	}
	logger := log.New(os.Stdout, "[MIGRATE] ", log.LstdFlags)
	return mongomigrations.NewMigration(client.Database(), logger, migCfg).RunMigrations()
}
