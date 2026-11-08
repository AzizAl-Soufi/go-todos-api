package versions

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
)

// Projects and collections (todos module), and the new todo fields: existing
// todos get a status from their completed flag, which is then removed.
func init() {
	migrations.Register(4, "projects_collections", upProjectsCollections, downProjectsCollections)
}

func upProjectsCollections(db *mongo.Database) error {
	ctx := context.Background()
	byUserCreated := bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: 1}}

	for _, name := range []string{"collections", "projects"} {
		if _, err := db.Collection(name).Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys: byUserCreated, Options: options.Index().SetName("idx_user_created"),
		}); err != nil {
			return err
		}
	}
	if _, err := db.Collection("projects").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "collectionId", Value: 1}}, Options: options.Index().SetName("idx_collection"),
	}); err != nil {
		return err
	}
	if _, err := db.Collection("todos").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "userId", Value: 1}, {Key: "projectId", Value: 1}}, Options: options.Index().SetName("idx_user_project")},
		{Keys: bson.D{{Key: "userId", Value: 1}, {Key: "dueAt", Value: 1}}, Options: options.Index().SetName("idx_user_due")},
		{Keys: bson.D{{Key: "userId", Value: 1}, {Key: "tags", Value: 1}}, Options: options.Index().SetName("idx_user_tags")},
	}); err != nil {
		return err
	}

	// Backfill todos written before this migration.
	completed := bson.M{"$eq": bson.A{"$completed", true}}
	_, err := db.Collection("todos").UpdateMany(ctx, bson.M{"status": bson.M{"$exists": false}}, mongo.Pipeline{
		{{Key: "$set", Value: bson.M{
			"status":      bson.M{"$cond": bson.A{completed, "done", "pending"}},
			"completedAt": bson.M{"$cond": bson.A{completed, "$createdAt", nil}},
			"updatedAt":   "$createdAt",
			"projectId":   nil,
			"notes":       "",
			"priority":    0,
			"tags":        bson.A{},
			"dueAt":       nil,
		}}},
		{{Key: "$unset", Value: "completed"}},
	})
	return err
}

func downProjectsCollections(db *mongo.Database) error {
	ctx := context.Background()
	_, err := db.Collection("todos").UpdateMany(ctx, bson.M{}, mongo.Pipeline{
		{{Key: "$set", Value: bson.M{"completed": bson.M{"$eq": bson.A{"$status", "done"}}}}},
		{{Key: "$unset", Value: bson.A{"status", "completedAt", "updatedAt", "projectId", "notes", "priority", "tags", "dueAt"}}},
	})
	if err != nil {
		return err
	}
	for _, name := range []string{"idx_user_project", "idx_user_due", "idx_user_tags"} {
		if err := db.Collection("todos").Indexes().DropOne(ctx, name); err != nil {
			return err
		}
	}
	if err := db.Collection("projects").Drop(ctx); err != nil {
		return err
	}
	return db.Collection("collections").Drop(ctx)
}
