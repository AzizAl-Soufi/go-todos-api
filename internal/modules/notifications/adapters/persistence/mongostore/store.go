// Package mongostore holds the MongoDB device store. Calls made with a
// transactional context join it.
package mongostore

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const DevicesCollection = "devices"

// Devices: one document per token (_id).
type Devices struct{ coll *mongo.Collection }

var _ application.DeviceRepository = (*Devices)(nil)

func NewDevices(db *mongo.Database) *Devices { return &Devices{coll: db.Collection(DevicesCollection)} }

type deviceDoc struct {
	Token     string        `bson:"_id"`
	UserID    bson.ObjectID `bson:"userId"`
	Platform  string        `bson:"platform"`
	CreatedAt time.Time     `bson:"createdAt"`
	UpdatedAt time.Time     `bson:"updatedAt"`
}

func (s *Devices) Save(ctx context.Context, d *domain.Device) error {
	oid, err := ids.ObjectID(d.UserID)
	if err != nil {
		return err
	}
	_, err = s.coll.UpdateOne(ctx, bson.M{"_id": d.Token}, bson.M{
		"$set":         bson.M{"userId": oid, "platform": string(d.Platform), "updatedAt": d.UpdatedAt},
		"$setOnInsert": bson.M{"createdAt": d.CreatedAt},
	}, options.UpdateOne().SetUpsert(true))
	return err
}

func (s *Devices) find(ctx context.Context, filter bson.M) ([]*domain.Device, error) {
	byRegistration := bson.D{{Key: "updatedAt", Value: 1}, {Key: "_id", Value: 1}}
	cur, err := s.coll.Find(ctx, filter, options.Find().SetSort(byRegistration))
	if err != nil {
		return nil, err
	}
	var docs []deviceDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]*domain.Device, 0, len(docs))
	for _, d := range docs {
		out = append(out, &domain.Device{
			Token:     d.Token,
			UserID:    d.UserID.Hex(),
			Platform:  domain.Platform(d.Platform),
			CreatedAt: d.CreatedAt,
			UpdatedAt: d.UpdatedAt,
		})
	}
	return out, nil
}

func (s *Devices) ListForUser(ctx context.Context, userID string) ([]*domain.Device, error) {
	oid, err := ids.ObjectID(userID)
	if err != nil {
		return nil, err
	}
	return s.find(ctx, bson.M{"userId": oid})
}

func (s *Devices) ListForUsers(ctx context.Context, userIDs []string) ([]*domain.Device, error) {
	oids := make([]bson.ObjectID, 0, len(userIDs))
	for _, id := range userIDs {
		oid, err := ids.ObjectID(id)
		if err != nil {
			return nil, err
		}
		oids = append(oids, oid)
	}
	return s.find(ctx, bson.M{"userId": bson.M{"$in": oids}})
}

func (s *Devices) Delete(ctx context.Context, userID, token string) error {
	oid, err := ids.ObjectID(userID)
	if err != nil {
		return err
	}
	res, err := s.coll.DeleteOne(ctx, bson.M{"_id": token, "userId": oid})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (s *Devices) DeleteTokens(ctx context.Context, tokens []string) error {
	if len(tokens) == 0 {
		return nil
	}
	_, err := s.coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": tokens}})
	return err
}

func (s *Devices) DeleteForUser(ctx context.Context, userID string) error {
	oid, err := ids.ObjectID(userID)
	if err != nil {
		return err
	}
	_, err = s.coll.DeleteMany(ctx, bson.M{"userId": oid})
	return err
}
