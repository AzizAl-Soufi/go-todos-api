package migrations

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (m *Migration) createIndexes(config *MigrationConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, collDef := range config.MongoDB.Collections {
		if len(collDef.Indexes) == 0 {
			continue
		}

		var mongoIndexes []mongo.IndexModel
		for _, idxDef := range collDef.Indexes {
			// Build the compound keys maintaining the order defined in YAML
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

		// Apply indexes to the collection
		collection := m.db.Collection(collDef.Name)
		_, err := collection.Indexes().CreateMany(ctx, mongoIndexes)
		if err != nil {
			m.log.Printf("Failed to create indexes for collection %s: %v", collDef.Name, err)
			return err
		}

		m.log.Printf("Applied %d index(es) to collection: %s", len(mongoIndexes), collDef.Name)
	}

	return nil
}
