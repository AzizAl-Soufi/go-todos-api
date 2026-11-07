package migrations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (m *Migration) createIndexes(config *MigrationConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	schemas, err := LoadSchemas(config.MongoDB.SchemaDir)
	if err != nil {
		return fmt.Errorf("failed to load schemas for indexing: %w", err)
	}

	for _, schema := range schemas {
		if len(schema.Indexes) == 0 {
			continue // Skip collections with no indexes defined
		}

		var mongoIndexes []mongo.IndexModel
		for _, idxDef := range schema.Indexes {
			keys := bson.D{}
			for _, k := range idxDef.Keys {
				keys = append(keys, bson.E{Key: k.Field, Value: k.Order})
			}

			idxOptions := options.Index()
			if idxDef.Unique {
				idxOptions.SetUnique(true)
			}

			mongoIndexes = append(mongoIndexes, mongo.IndexModel{
				Keys:    keys,
				Options: idxOptions,
			})
		}

		collection := m.db.Collection(schema.Collection)
		_, err := collection.Indexes().CreateMany(ctx, mongoIndexes)
		if err != nil {
			if isIndexConflictError(err) {
				m.log.Printf("⚠️ INDEX CONFLICT in collection '%s': %v", schema.Collection, err)
				m.log.Printf("To resolve: Manually drop the conflicting index in MongoDB, then re-run the migration.")
				return err
			}

			m.log.Printf("Failed to create indexes for collection %s: %v", schema.Collection, err)
			return err
		}

		m.log.Printf("Applied %d index(es) to collection: %s", len(mongoIndexes), schema.Collection)
	}

	return nil
}

func isIndexConflictError(err error) bool {
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) {
		if cmdErr.Code == 85 || strings.Contains(cmdErr.Message, "Index with name") {
			return true
		}
	}
	return false
}