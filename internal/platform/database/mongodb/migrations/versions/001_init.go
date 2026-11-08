package versions

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
)

func init() {
	migrations.Register(1, "initialize_schema",
		func(db *mongo.Database) error {
			// Explicitly create collections. MongoDB creates them implicitly on insert,
			// but doing it here guarantees they exist for the indexing step.
			if err := db.CreateCollection(context.Background(), "users"); err != nil {
				// Ignore if it already exists
			}
			if err := db.CreateCollection(context.Background(), "todos"); err != nil {
				// Ignore if it already exists
			}
			return nil
		},
		func(db *mongo.Database) error {
			db.Collection("todos").Drop(context.Background())
			db.Collection("users").Drop(context.Background())
			return nil
		},
	)
}
