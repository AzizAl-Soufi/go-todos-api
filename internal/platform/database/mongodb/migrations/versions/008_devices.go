package versions

import (
	"context"
	"slices"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Devices that receive push notifications (notifications module): one
// document per FCM registration token (_id), listed per user. Created up
// front because older servers can't create a collection inside a transaction.
func init() {
	migrations.Register(8, "devices", upDevices, downDevices)
}

func upDevices(db *mongo.Database) error {
	ctx := context.Background()
	existing, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return err
	}
	if !slices.Contains(existing, "devices") {
		if err := db.CreateCollection(ctx, "devices"); err != nil {
			return err
		}
	}
	_, err = db.Collection("devices").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "updatedAt", Value: 1}},
		Options: options.Index().SetName("idx_user_updated"),
	})
	return err
}

func downDevices(db *mongo.Database) error {
	return db.Collection("devices").Drop(context.Background())
}
