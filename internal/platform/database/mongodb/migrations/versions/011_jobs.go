package versions

import (
	"context"
	"slices"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
)

// Background jobs (platform/jobs): the transactional outbox and job queue,
// and the leases that elect one scheduler among the worker processes.
// uniqueKey dedupes events and schedule slots; jobs without one leave the
// field out, so a sparse index makes it unique only where set.
func init() {
	migrations.Register(11, "jobs", upJobs, downJobs)
}

var jobsIndexes = map[string][]mongo.IndexModel{
	"jobs": {
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "runAt", Value: 1}}, Options: options.Index().SetName("idx_status_run_at")},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "lockedUntil", Value: 1}}, Options: options.Index().SetName("idx_status_locked_until")},
		{Keys: bson.D{{Key: "uniqueKey", Value: 1}}, Options: options.Index().SetName("uniq_unique_key").SetUnique(true).SetSparse(true)},
		{Keys: bson.D{{Key: "finishedAt", Value: 1}}, Options: options.Index().SetName("idx_finished_at").SetSparse(true)},
	},
	"job_leases": nil,
}

func upJobs(db *mongo.Database) error {
	ctx := context.Background()
	existing, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return err
	}
	for name, indexes := range jobsIndexes {
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

func downJobs(db *mongo.Database) error {
	ctx := context.Background()
	for name := range jobsIndexes {
		if err := db.Collection(name).Drop(ctx); err != nil {
			return err
		}
	}
	return nil
}
