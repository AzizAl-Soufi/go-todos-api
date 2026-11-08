package versions

import (
	"context"
	"slices"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// The OAuth 2.1 authorization server for MCP clients (oauth module):
// registered clients, authorization codes (by hash, removed by a TTL index
// once expired) and grants (removed by a TTL index once their refresh token
// expired), listed per user. Collections are created up front because older
// servers can't create one inside a transaction.
func init() {
	migrations.Register(10, "oauth", upOAuth, downOAuth)
}

var oauthIndexes = map[string][]mongo.IndexModel{
	"oauth_clients": nil,
	"oauth_codes": {
		{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetName("ttl_expires_at").SetExpireAfterSeconds(0)},
		{Keys: bson.D{{Key: "userId", Value: 1}}, Options: options.Index().SetName("idx_user")},
	},
	"oauth_grants": {
		{Keys: bson.D{{Key: "userId", Value: 1}, {Key: "lastUsedAt", Value: -1}}, Options: options.Index().SetName("idx_user_used")},
		{Keys: bson.D{{Key: "refreshExpiresAt", Value: 1}}, Options: options.Index().SetName("ttl_refresh_expires_at").SetExpireAfterSeconds(0)},
	},
}

func upOAuth(db *mongo.Database) error {
	ctx := context.Background()
	existing, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return err
	}
	for name, indexes := range oauthIndexes {
		if !slices.Contains(existing, name) {
			if err := db.CreateCollection(ctx, name); err != nil {
				return err
			}
		}
		if len(indexes) > 0 {
			if _, err := db.Collection(name).Indexes().CreateMany(ctx, indexes); err != nil {
				return err
			}
		}
	}
	return nil
}

func downOAuth(db *mongo.Database) error {
	ctx := context.Background()
	for name := range oauthIndexes {
		if err := db.Collection(name).Drop(ctx); err != nil {
			return err
		}
	}
	return nil
}
