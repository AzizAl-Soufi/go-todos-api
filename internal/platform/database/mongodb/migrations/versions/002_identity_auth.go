package versions

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
)

// Collections owned by the identity module for refresh-token sessions and
// email verification. TTL indexes let MongoDB delete expired documents.
func init() {
	migrations.Register(2, "identity_auth", upIdentityAuth, downIdentityAuth)
}

func upIdentityAuth(db *mongo.Database) error {
	ctx := context.Background()

	_, err := db.Collection("sessions").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "tokenHash", Value: 1}}, Options: options.Index().SetName("uniq_token_hash").SetUnique(true)},
		{Keys: bson.D{{Key: "userId", Value: 1}}, Options: options.Index().SetName("idx_user")},
		{Keys: bson.D{{Key: "familyId", Value: 1}}, Options: options.Index().SetName("idx_family")},
		{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetName("ttl_expires_at").SetExpireAfterSeconds(0)},
	})
	if err != nil {
		return err
	}

	_, err = db.Collection("email_verifications").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "tokenHash", Value: 1}}, Options: options.Index().SetName("uniq_token_hash").SetUnique(true)},
		{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetName("ttl_expires_at").SetExpireAfterSeconds(0)},
	})
	return err
}

func downIdentityAuth(db *mongo.Database) error {
	ctx := context.Background()
	if err := db.Collection("email_verifications").Drop(ctx); err != nil {
		return err
	}
	return db.Collection("sessions").Drop(ctx)
}
