package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit"
	auditmem "github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/memstore"
	auditmongo "github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/mongostore"
	auditpg "github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/pgstore"
	identitymem "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/memstore"
	identitymongo "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/mongostore"
	identitypg "github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
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
	"github.com/jackc/pgx/v5/pgxpool"
)

// storage bundles every persistence adapter for one backend.
type storage struct {
	todos          todos.TodoStore
	projects       todos.ProjectStore
	collections    todos.CollectionStore
	users          application.UserRepository
	sessions       application.SessionRepository
	verifications  application.OneTimeTokenRepository
	passwordResets application.OneTimeTokenRepository
	passkeys       application.PasskeyRepository
	challenges     application.PasskeyChallengeRepository
	teams          teams.TeamStore
	members        teams.MemberStore
	invitations    teams.InvitationStore
	events         audit.EventStore
	tx             tx.Runner
	close          func()
}

func newStorage(ctx context.Context, cfg *config.Config) (*storage, error) {
	switch cfg.DB.DBType {
	case "memory":
		return newMemoryStorage(), nil
	case "mongo":
		return newMongoStorage(ctx, cfg)
	case "postgres":
		return newPostgresStorage(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported DB_TYPE %q (want memory, mongo or postgres)", cfg.DB.DBType)
	}
}

func newMemoryStorage() *storage {
	return &storage{
		todos:          todosmem.NewTodos(),
		projects:       todosmem.NewProjects(),
		collections:    todosmem.NewCollections(),
		users:          identitymem.NewUsers(),
		sessions:       identitymem.NewSessions(),
		verifications:  identitymem.NewVerifications(),
		passwordResets: identitymem.NewPasswordResets(),
		passkeys:       identitymem.NewPasskeys(),
		challenges:     identitymem.NewPasskeyChallenges(),
		teams:          teamsmem.NewTeams(),
		members:        teamsmem.NewMembers(),
		invitations:    teamsmem.NewInvitations(),
		events:         auditmem.NewEvents(),
		tx:             tx.None{},
		close:          func() {},
	}
}

func newMongoStorage(ctx context.Context, cfg *config.Config) (*storage, error) {
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
		if err := migrateMongo(client, cfg.DB.MongoMigrationsConfig); err != nil {
			return nil, err
		}
	}

	store := mongoStorage(ctx, client)
	store.close = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Close(ctx); err != nil {
			slog.Error("closing MongoDB connection", "error", err)
		}
	}
	return store, nil
}

// mongoStorage builds the adapters on a connected, migrated database.
func mongoStorage(ctx context.Context, client mongodb.MongoDBClient) *storage {
	db := client.Database()
	return &storage{
		todos:          todosmongo.NewTodos(db),
		projects:       todosmongo.NewProjects(db),
		collections:    todosmongo.NewCollections(db),
		users:          identitymongo.NewUsers(db),
		sessions:       identitymongo.NewSessions(db),
		verifications:  identitymongo.NewVerifications(db),
		passwordResets: identitymongo.NewPasswordResets(db),
		passkeys:       identitymongo.NewPasskeys(db),
		challenges:     identitymongo.NewPasskeyChallenges(db),
		teams:          teamsmongo.NewTeams(db),
		members:        teamsmongo.NewMembers(db),
		invitations:    teamsmongo.NewInvitations(db),
		events:         auditmongo.NewEvents(db),
		tx:             mongodb.NewTxRunner(ctx, client),
		close:          func() {},
	}
}

func newPostgresStorage(ctx context.Context, cfg *config.Config) (*storage, error) {
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
			client.Close()
			return nil, err
		}
	}

	store := postgresStorage(pool)
	store.close = func() {
		if err := client.Close(); err != nil {
			slog.Error("closing PostgreSQL connection", "error", err)
		}
	}
	return store, nil
}

// postgresStorage builds the adapters on a connected, migrated pool.
func postgresStorage(pool *pgxpool.Pool) *storage {
	return &storage{
		todos:          todospg.NewTodos(pool),
		projects:       todospg.NewProjects(pool),
		collections:    todospg.NewCollections(pool),
		users:          identitypg.NewUsers(pool),
		sessions:       identitypg.NewSessions(pool),
		verifications:  identitypg.NewVerifications(pool),
		passwordResets: identitypg.NewPasswordResets(pool),
		passkeys:       identitypg.NewPasskeys(pool),
		challenges:     identitypg.NewPasskeyChallenges(pool),
		teams:          teamspg.NewTeams(pool),
		members:        teamspg.NewMembers(pool),
		invitations:    teamspg.NewInvitations(pool),
		events:         auditpg.NewEvents(pool),
		tx:             postgres.NewTxRunner(pool),
		close:          func() {},
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
