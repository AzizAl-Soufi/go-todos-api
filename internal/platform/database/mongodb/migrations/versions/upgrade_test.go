package versions_test

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
)

// TestUpgrade_TodosGainStatus checks migration 4 on todos written before it:
// the completed flag becomes a status, and rolling back restores the flag.
// Needs TEST_MONGO_URI.
func TestUpgrade_TodosGainStatus(t *testing.T) {
	db := dbtest.Mongo(t).Database()
	ctx := context.Background()
	todos := db.Collection("todos")
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := todos.InsertMany(ctx, []any{
		bson.M{"userId": bson.NewObjectID(), "title": "finished", "completed": true, "createdAt": created},
		bson.M{"userId": bson.NewObjectID(), "title": "open", "completed": false, "createdAt": created},
	}); err != nil {
		t.Fatal(err)
	}

	var m4 migrations.MigrationEntry
	for _, m := range migrations.GetMigrations() {
		if m.Version == 4 {
			m4 = m
		}
	}
	if err := m4.Migrate(db); err != nil { // already applied once on the empty database: must be re-runnable
		t.Fatal(err)
	}

	check := func(title, status string, completedAt bool) {
		t.Helper()
		var doc bson.M
		if err := todos.FindOne(ctx, bson.M{"title": title}).Decode(&doc); err != nil {
			t.Fatal(err)
		}
		_, hasCompleted := doc["completed"]
		if doc["status"] != status || (doc["completedAt"] != nil) != completedAt || hasCompleted || doc["updatedAt"] == nil {
			t.Fatalf("%s after upgrade: %v", title, doc)
		}
	}
	check("finished", "done", true)
	check("open", "pending", false)

	if err := m4.Rollback(db); err != nil {
		t.Fatal(err)
	}
	var doc bson.M
	if err := todos.FindOne(ctx, bson.M{"title": "finished"}).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if _, hasStatus := doc["status"]; doc["completed"] != true || hasStatus {
		t.Fatalf("after rollback: %v", doc)
	}
}
