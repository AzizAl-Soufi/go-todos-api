package migrations

import (
	"context"
	"errors"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type SchemaVersion struct {
	Version   int       `bson:"version"`
	AppliedAt time.Time `bson:"applied_at"`
	Status    string    `bson:"status"`
}

type Migration struct {
	db     *mongo.Database
	log    *log.Logger
	config *MigrationConfig
}

func NewMigration(db *mongo.Database, logger *log.Logger, cfg *MigrationConfig) *Migration {
	return &Migration{db: db, log: logger, config: cfg}
}

func (m *Migration) RunMigrations() error {
	m.log.Println("Starting database migration process...")

	if err := m.runSchemaUp(); err != nil {
		return err
	}
	if err := m.createIndexes(m.config); err != nil {
		return err
	}
	if err := m.seedData(m.config); err != nil {
		return err
	}

	return nil
}

func (m *Migration) runSchemaUp() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var current SchemaVersion
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})

	// Check for the most recent applied migration
	err := m.db.Collection("schema_versions").FindOne(ctx, bson.M{"status": "applied"}, opts).Decode(&current)
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return err
	}

	for _, entry := range GetMigrations() {
		if entry.Version > current.Version {
			m.log.Printf("Applying migration %d: %s\n", entry.Version, entry.Name)
			if err := entry.Migrate(m.db); err != nil {
				return err
			}

			_, err = m.db.Collection("schema_versions").InsertOne(ctx, SchemaVersion{
				Version:   entry.Version,
				AppliedAt: time.Now(),
				Status:    "applied",
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
