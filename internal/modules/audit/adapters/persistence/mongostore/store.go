// Package mongostore holds the MongoDB audit event store. Calls made with a
// transactional context join it.
package mongostore

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const EventsCollection = "audit_events"

type Events struct{ coll *mongo.Collection }

var _ application.EventStore = (*Events)(nil)

func NewEvents(db *mongo.Database) *Events { return &Events{coll: db.Collection(EventsCollection)} }

type eventDoc struct {
	ID         bson.ObjectID     `bson:"_id"`
	TeamID     *bson.ObjectID    `bson:"teamId"`
	UserID     *bson.ObjectID    `bson:"userId"`
	ActorID    *bson.ObjectID    `bson:"actorId"`
	Action     string            `bson:"action"`
	TargetType string            `bson:"targetType"`
	TargetID   string            `bson:"targetId"`
	Details    map[string]string `bson:"details"`
	IP         string            `bson:"ip"`
	UserAgent  string            `bson:"userAgent"`
	CreatedAt  bson.DateTime     `bson:"createdAt"`
}

func optionalOID(id string) (*bson.ObjectID, error) {
	if id == "" {
		return nil, nil
	}
	oid, err := ids.ObjectID(id)
	if err != nil {
		return nil, err
	}
	return &oid, nil
}

func hexOrEmpty(id *bson.ObjectID) string {
	if id == nil {
		return ""
	}
	return id.Hex()
}

func (s *Events) Append(ctx context.Context, e *domain.Event) error {
	doc := eventDoc{
		ID: bson.NewObjectID(), Action: e.Action, TargetType: e.TargetType, TargetID: e.TargetID,
		Details: e.Details, IP: e.IP, UserAgent: e.UserAgent, CreatedAt: bson.NewDateTimeFromTime(e.CreatedAt),
	}
	if doc.Details == nil {
		doc.Details = map[string]string{}
	}
	for _, p := range []struct {
		dst **bson.ObjectID
		id  string
	}{{&doc.TeamID, e.TeamID}, {&doc.UserID, e.UserID}, {&doc.ActorID, e.ActorID}} {
		oid, err := optionalOID(p.id)
		if err != nil {
			return err
		}
		*p.dst = oid
	}
	if _, err := s.coll.InsertOne(ctx, doc); err != nil {
		return err
	}
	e.ID = doc.ID.Hex()
	return nil
}

func (s *Events) List(ctx context.Context, f domain.Filter) ([]*domain.Event, error) {
	and := bson.A{}
	for key, id := range map[string]string{"teamId": f.TeamID, "userId": f.UserID} {
		if id == "" {
			continue
		}
		oid, err := ids.ObjectID(id)
		if err != nil {
			return nil, err
		}
		and = append(and, bson.M{key: oid})
	}
	if f.Before != nil {
		oid, err := ids.ObjectID(f.Before.ID)
		if err != nil {
			return nil, err
		}
		at := bson.NewDateTimeFromTime(f.Before.At)
		and = append(and, bson.M{"$or": bson.A{
			bson.M{"createdAt": bson.M{"$lt": at}},
			bson.M{"createdAt": at, "_id": bson.M{"$lt": oid}},
		}})
	}
	filter := bson.M{}
	if len(and) > 0 {
		filter["$and"] = and
	}
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}})
	if f.Limit > 0 {
		opts.SetLimit(int64(f.Limit))
	}
	cursor, err := s.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	var docs []eventDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]*domain.Event, 0, len(docs))
	for _, d := range docs {
		details := d.Details
		if details == nil {
			details = map[string]string{}
		}
		out = append(out, &domain.Event{
			ID: d.ID.Hex(), TeamID: hexOrEmpty(d.TeamID), UserID: hexOrEmpty(d.UserID), ActorID: hexOrEmpty(d.ActorID),
			Action: d.Action, TargetType: d.TargetType, TargetID: d.TargetID, Details: details,
			IP: d.IP, UserAgent: d.UserAgent, CreatedAt: d.CreatedAt.Time().UTC(),
		})
	}
	return out, nil
}

func (s *Events) deleteBy(ctx context.Context, key, id string) error {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return err
	}
	_, err = s.coll.DeleteMany(ctx, bson.M{key: oid})
	return err
}

func (s *Events) DeleteByUser(ctx context.Context, userID string) error {
	return s.deleteBy(ctx, "userId", userID)
}

func (s *Events) DeleteByTeam(ctx context.Context, teamID string) error {
	return s.deleteBy(ctx, "teamId", teamID)
}
