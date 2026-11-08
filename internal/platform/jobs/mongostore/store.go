// Package mongostore is the MongoDB job store. Calls made with a
// transactional context join it, so enqueueing is part of the caller's unit
// of work. Claiming takes one job at a time with findOneAndUpdate, which is
// atomic, so concurrent workers never get the same job.
package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
)

const (
	JobsCollection   = "jobs"
	LeasesCollection = "job_leases"
)

type Store struct {
	jobs   *mongo.Collection
	leases *mongo.Collection
}

var _ jobs.Store = (*Store)(nil)

func New(db *mongo.Database) *Store {
	return &Store{jobs: db.Collection(JobsCollection), leases: db.Collection(LeasesCollection)}
}

type jobDoc struct {
	ID          bson.ObjectID `bson:"_id"`
	Kind        string        `bson:"kind"`
	Payload     []byte        `bson:"payload"`
	UniqueKey   string        `bson:"uniqueKey,omitempty"` // sparse unique index
	OwnerID     string        `bson:"ownerId"`
	Status      string        `bson:"status"`
	Attempts    int           `bson:"attempts"`
	MaxAttempts int           `bson:"maxAttempts"`
	RunAt       time.Time     `bson:"runAt"`
	LockedUntil *time.Time    `bson:"lockedUntil,omitempty"`
	LastError   string        `bson:"lastError"`
	CreatedAt   time.Time     `bson:"createdAt"`
	UpdatedAt   time.Time     `bson:"updatedAt"`
	FinishedAt  *time.Time    `bson:"finishedAt,omitempty"`
}

func (d jobDoc) toJob() *jobs.Job {
	j := &jobs.Job{
		ID: d.ID.Hex(), Kind: d.Kind, Payload: d.Payload, UniqueKey: d.UniqueKey, OwnerID: d.OwnerID,
		Status: jobs.Status(d.Status), Attempts: d.Attempts, MaxAttempts: d.MaxAttempts, RunAt: d.RunAt.UTC(),
		LastError: d.LastError, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC(),
	}
	if d.LockedUntil != nil {
		j.LockedUntil = d.LockedUntil.UTC()
	}
	if d.FinishedAt != nil {
		j.FinishedAt = d.FinishedAt.UTC()
	}
	return j
}

func (s *Store) Enqueue(ctx context.Context, j *jobs.Job) error {
	if j.UniqueKey != "" {
		// Look first: inside a transaction a duplicate key error would abort it.
		var existing jobDoc
		err := s.jobs.FindOne(ctx, bson.M{"uniqueKey": j.UniqueKey}, options.FindOne().SetProjection(bson.M{"_id": 1})).Decode(&existing)
		if err == nil {
			j.ID = existing.ID.Hex()
			return nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return err
		}
	}
	doc := jobDoc{
		ID: bson.NewObjectID(), Kind: j.Kind, Payload: j.Payload, UniqueKey: j.UniqueKey, OwnerID: j.OwnerID,
		Status: string(jobs.Queued), MaxAttempts: j.MaxAttempts, RunAt: j.RunAt, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
	_, err := s.jobs.InsertOne(ctx, doc)
	if mongo.IsDuplicateKeyError(err) && j.UniqueKey != "" {
		// Another enqueue with the key won the race.
		var existing jobDoc
		if err := s.jobs.FindOne(ctx, bson.M{"uniqueKey": j.UniqueKey}).Decode(&existing); err != nil {
			return err
		}
		j.ID = existing.ID.Hex()
		return nil
	}
	if err != nil {
		return err
	}
	j.ID = doc.ID.Hex()
	return nil
}

func (s *Store) Claim(ctx context.Context, c jobs.Claim) ([]*jobs.Job, error) {
	filter := bson.M{"$or": bson.A{
		bson.M{"status": string(jobs.Queued), "runAt": bson.M{"$lte": c.Now}},
		bson.M{"status": string(jobs.Running), "lockedUntil": bson.M{"$lt": c.Now}},
	}}
	update := bson.M{"$set": bson.M{"status": string(jobs.Running), "lockedUntil": c.Now.Add(c.Lease), "updatedAt": c.Now}}
	if c.CountAttempt {
		update["$inc"] = bson.M{"attempts": 1}
	}
	opts := options.FindOneAndUpdate().
		SetSort(bson.D{{Key: "runAt", Value: 1}, {Key: "_id", Value: 1}}).
		SetReturnDocument(options.After)
	out := []*jobs.Job{}
	for range c.Limit {
		var d jobDoc
		err := s.jobs.FindOneAndUpdate(ctx, filter, update, opts).Decode(&d)
		if errors.Is(err, mongo.ErrNoDocuments) {
			break
		}
		if err != nil {
			return out, err
		}
		out = append(out, d.toJob())
	}
	return out, nil
}

func (s *Store) set(ctx context.Context, id string, set bson.M, unset ...string) error {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return err
	}
	update := bson.M{"$set": set}
	if len(unset) > 0 {
		u := bson.M{}
		for _, f := range unset {
			u[f] = ""
		}
		update["$unset"] = u
	}
	res, err := s.jobs.UpdateOne(ctx, bson.M{"_id": oid}, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (s *Store) Succeed(ctx context.Context, id string, at time.Time) error {
	return s.set(ctx, id, bson.M{"status": string(jobs.Succeeded), "finishedAt": at, "updatedAt": at}, "lockedUntil")
}

func (s *Store) Fail(ctx context.Context, id string, at time.Time, errMsg string) error {
	return s.set(ctx, id, bson.M{"status": string(jobs.Failed), "finishedAt": at, "updatedAt": at, "lastError": errMsg}, "lockedUntil")
}

func (s *Store) Retry(ctx context.Context, id string, runAt, at time.Time, errMsg string) error {
	return s.set(ctx, id, bson.M{"status": string(jobs.Queued), "runAt": runAt, "updatedAt": at, "lastError": errMsg}, "lockedUntil")
}

func (s *Store) Dispatch(ctx context.Context, id string, at time.Time) error {
	return s.set(ctx, id, bson.M{"status": string(jobs.Dispatched), "updatedAt": at}, "lockedUntil")
}

func (s *Store) Attempted(ctx context.Context, id string, attempts int, at time.Time, errMsg string) error {
	return s.set(ctx, id, bson.M{"attempts": attempts, "updatedAt": at, "lastError": errMsg})
}

func (s *Store) Get(ctx context.Context, id string) (*jobs.Job, error) {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return nil, err
	}
	var d jobDoc
	if err := s.jobs.FindOne(ctx, bson.M{"_id": oid}).Decode(&d); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	return d.toJob(), nil
}

func (s *Store) DeleteFinished(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.jobs.DeleteMany(ctx, bson.M{"finishedAt": bson.M{"$lt": before}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func (s *Store) Counts(ctx context.Context, since time.Time) (jobs.Counts, error) {
	c := jobs.Counts{ByStatus: map[jobs.Status]int64{}}
	cur, err := s.jobs.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$group", Value: bson.M{"_id": "$status", "n": bson.M{"$sum": 1}}}},
	})
	if err != nil {
		return c, err
	}
	var groups []struct {
		Status string `bson:"_id"`
		N      int64  `bson:"n"`
	}
	if err := cur.All(ctx, &groups); err != nil {
		return c, err
	}
	for _, g := range groups {
		c.ByStatus[jobs.Status(g.Status)] = g.N
	}
	if c.FailedSince, err = s.jobs.CountDocuments(ctx, bson.M{"status": string(jobs.Failed), "finishedAt": bson.M{"$gte": since}}); err != nil {
		return c, err
	}
	var oldest jobDoc
	err = s.jobs.FindOne(ctx, bson.M{"status": string(jobs.Queued)}, options.FindOne().SetSort(bson.D{{Key: "runAt", Value: 1}})).Decode(&oldest)
	switch {
	case err == nil:
		c.OldestQueued = oldest.RunAt.UTC()
	case !errors.Is(err, mongo.ErrNoDocuments):
		return c, err
	}
	return c, nil
}

func (s *Store) AcquireLease(ctx context.Context, name, holder string, until, now time.Time) (bool, error) {
	filter := bson.M{"_id": name, "$or": bson.A{bson.M{"expiresAt": bson.M{"$lt": now}}, bson.M{"holder": holder}}}
	update := bson.M{"$set": bson.M{"holder": holder, "expiresAt": until}}
	err := s.leases.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetUpsert(true)).Err()
	switch {
	case err == nil, errors.Is(err, mongo.ErrNoDocuments): // updated, or inserted (no document before)
		return true, nil
	case mongo.IsDuplicateKeyError(err): // someone else's live lease
		return false, nil
	}
	return false, err
}

func (s *Store) ReleaseLease(ctx context.Context, name, holder string) error {
	_, err := s.leases.DeleteOne(ctx, bson.M{"_id": name, "holder": holder})
	return err
}
