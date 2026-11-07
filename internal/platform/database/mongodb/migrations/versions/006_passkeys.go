package versions

import (
	"context"
	"slices"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Passkeys and pending passkey ceremonies (identity module). A credential ID
// is registered once across all accounts; the TTL index removes expired
// challenges (the store also prunes them). Collections are created up front
// because older servers can't create one inside a transaction.
func init() {
	migrations.Register(6, "passkeys", upPasskeys, downPasskeys)
}

var passkeysIndexes = map[string][]mongo.IndexModel{
	"passkeys": {
		{Keys: bson.D{{Key: "credentialId", Value: 1}}, Options: options.Index().SetName("uniq_credential_id").SetUnique(true)},
		{Keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}, Options: options.Index().SetName("idx_user_created")},
	},
	"passkey_challenges": {
		{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetName("ttl_expires_at").SetExpireAfterSeconds(0)},
		{Keys: bson.D{{Key: "userId", Value: 1}}, Options: options.Index().SetName("idx_user")},
	},
}

func upPasskeys(db *mongo.Database) error {
	ctx := context.Background()
	existing, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return err
	}
	for name, indexes := range passkeysIndexes {
		if !slices.Contains(existing, name) {
			if err := db.CreateCollection(ctx, name); err != nil {
				return err
			}
		}
		if _, err := db.Collection(name).Indexes().CreateMany(ctx, indexes); err != nil {
			return err
		}
	}
	return nil
}

func downPasskeys(db *mongo.Database) error {
	ctx := context.Background()
	for name := range passkeysIndexes {
		if err := db.Collection(name).Drop(ctx); err != nil {
			return err
		}
	}
	return nil
}
