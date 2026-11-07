package migrations

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func (m *Migration) seedData(config *MigrationConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, seedDef := range config.MongoDB.Seeding.Files {
		// Only seed if the collection is empty
		count, err := m.db.Collection(seedDef.Collection).CountDocuments(ctx, bson.M{})
		if err != nil {
			return fmt.Errorf("failed to count documents in %s: %w", seedDef.Collection, err)
		}

		if count > 0 {
			continue // Skip seeding, data already exists
		}

		// Resolve the full path to the JSON file
		filePath := filepath.Join(config.MongoDB.Seeding.Dir, seedDef.File)
		data, err := os.ReadFile(filePath)
		if err != nil {
			m.log.Printf("Warning: Seed file not found or unreadable %s: %v", filePath, err)
			continue
		}

		// Decode JSON into a generic slice of interface{} maps
		var documents []interface{}
		if err := json.Unmarshal(data, &documents); err != nil {
			return fmt.Errorf("failed to parse JSON from %s: %w", filePath, err)
		}

		if len(documents) > 0 {
			_, err = m.db.Collection(seedDef.Collection).InsertMany(ctx, documents)
			if err != nil {
				return fmt.Errorf("failed to insert seed data into %s: %w", seedDef.Collection, err)
			}
			m.log.Printf("Seeded %d documents into collection: %s", len(documents), seedDef.Collection)
		}
	}

	return nil
}
