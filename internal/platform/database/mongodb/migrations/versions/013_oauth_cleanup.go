package versions

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
)

// The oauth cleanup job deletes registered clients that are old and unused:
// it finds clients by createdAt and looks up the grants that reference each
// one by clientId. (Expired codes and grants already have TTL indexes.)
func init() {
	migrations.Register(13, "oauth_cleanup", upOAuthCleanup, downOAuthCleanup)
}

var oauthCleanupIndexes = []struct{ coll, name, key string }{
	{"oauth_clients", "idx_created_at", "createdAt"},
	{"oauth_grants", "idx_client", "clientId"},
}

func upOAuthCleanup(db *mongo.Database) error {
	ctx := context.Background()
	for _, ix := range oauthCleanupIndexes {
		model := mongo.IndexModel{Keys: bson.D{{Key: ix.key, Value: 1}}, Options: options.Index().SetName(ix.name)}
		if _, err := db.Collection(ix.coll).Indexes().CreateOne(ctx, model); err != nil {
			return err
		}
	}
	return nil
}

func downOAuthCleanup(db *mongo.Database) error {
	ctx := context.Background()
	for _, ix := range oauthCleanupIndexes {
		err := db.Collection(ix.coll).Indexes().DropOne(ctx, ix.name)
		var cmdErr mongo.CommandError
		if err != nil && !(errors.As(err, &cmdErr) && (cmdErr.Code == 27 || cmdErr.Code == 26)) { // IndexNotFound, NamespaceNotFound
			return err
		}
	}
	return nil
}
