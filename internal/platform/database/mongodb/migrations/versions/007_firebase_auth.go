package versions

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Sign-in moves to Firebase Authentication (identity module): an account is
// linked to its Firebase user by firebaseUid, unique among the accounts that
// have one (accounts from before stay unlinked until their first sign-in).
// passwordHash, tokenVersion and the sessions, email_verifications and
// password_resets collections are no longer used; they stay so this release
// can be rolled back, and a later migration removes them.
func init() {
	migrations.Register(7, "firebase_auth", upFirebaseAuth, downFirebaseAuth)
}

func upFirebaseAuth(db *mongo.Database) error {
	_, err := db.Collection("users").Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "firebaseUid", Value: 1}},
		Options: options.Index().SetName("uniq_firebase_uid").SetUnique(true).
			SetPartialFilterExpression(bson.M{"firebaseUid": bson.M{"$type": "string"}}),
	})
	return err
}

func downFirebaseAuth(db *mongo.Database) error {
	ctx := context.Background()
	if err := db.Collection("users").Indexes().DropOne(ctx, "uniq_firebase_uid"); err != nil {
		return err
	}
	_, err := db.Collection("users").UpdateMany(ctx, bson.M{}, bson.M{"$unset": bson.M{"firebaseUid": "", "sessionsValidAfter": ""}})
	return err
}
