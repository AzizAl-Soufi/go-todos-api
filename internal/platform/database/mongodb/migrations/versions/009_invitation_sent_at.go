package versions

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
)

// When each team invitation email was last sent (teams module): resending
// waits 2 minutes after the last send. Existing invitations were last sent
// when they were created. The store also reads a missing sentAt as
// createdAt, for documents the previous release writes during a deploy.
func init() {
	migrations.Register(9, "invitation_sent_at", upInvitationSentAt, downInvitationSentAt)
}

func upInvitationSentAt(db *mongo.Database) error {
	_, err := db.Collection("team_invitations").UpdateMany(context.Background(),
		bson.M{"sentAt": bson.M{"$exists": false}},
		bson.A{bson.M{"$set": bson.M{"sentAt": "$createdAt"}}},
	)
	return err
}

func downInvitationSentAt(db *mongo.Database) error {
	_, err := db.Collection("team_invitations").UpdateMany(context.Background(),
		bson.M{}, bson.M{"$unset": bson.M{"sentAt": ""}})
	return err
}
