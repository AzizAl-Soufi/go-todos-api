package versions

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
)

// Pending password resets (identity module): one document per user, only the
// token hash is stored, and the TTL index removes expired ones.
func init() {
	migrations.Register(3, "password_resets", upPasswordResets, downPasswordResets)
}

func upPasswordResets(db *mongo.Database) error {
	_, err := db.Collection("password_resets").Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{Keys: bson.D{{Key: "tokenHash", Value: 1}}, Options: options.Index().SetName("uniq_token_hash").SetUnique(true)},
		{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetName("ttl_expires_at").SetExpireAfterSeconds(0)},
	})
	return err
}

func downPasswordResets(db *mongo.Database) error {
	return db.Collection("password_resets").Drop(context.Background())
}
