package versions

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Due-date alerts (todos module): todos gain dueSoonAlertedFor and
// overdueAlertedFor, the deadline each alert was last sent for. They are
// written only when an alert goes out, so existing todos need no backfill
// (missing means never sent). The alert job scans open todos by deadline;
// the index holds only open todos with one, so done and cancelled todos
// never weigh on it.
func init() {
	migrations.Register(12, "todo_due_alerts", upTodoDueAlerts, downTodoDueAlerts)
}

const idxTodoDueAlerts = "idx_due_alerts"

func upTodoDueAlerts(db *mongo.Database) error {
	_, err := db.Collection("todos").Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{{Key: "dueAt", Value: 1}, {Key: "_id", Value: 1}},
		Options: options.Index().SetName(idxTodoDueAlerts).SetPartialFilterExpression(bson.M{
			"status": bson.M{"$in": bson.A{"pending", "in_progress", "blocked"}},
			"dueAt":  bson.M{"$type": "date"},
		}),
	})
	return err
}

func downTodoDueAlerts(db *mongo.Database) error {
	ctx := context.Background()
	if err := db.Collection("todos").Indexes().DropOne(ctx, idxTodoDueAlerts); err != nil {
		return err
	}
	_, err := db.Collection("todos").UpdateMany(ctx, bson.M{},
		bson.M{"$unset": bson.M{"dueSoonAlertedFor": "", "overdueAlertedFor": ""}})
	return err
}
