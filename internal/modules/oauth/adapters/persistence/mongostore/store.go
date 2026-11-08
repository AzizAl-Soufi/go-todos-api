// Package mongostore holds the oauth module's MongoDB stores. Calls made
// with a transactional context join it.
package mongostore

import (
	"context"
	"errors"
	"slices"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
)

const (
	ClientsCollection = "oauth_clients"
	CodesCollection   = "oauth_codes"
	GrantsCollection  = "oauth_grants"
)

type owner struct {
	UserID        bson.ObjectID `bson:"userId"`
	Name          string        `bson:"userName"`
	Email         string        `bson:"userEmail"`
	EmailVerified bool          `bson:"emailVerified"`
}

func ownerDoc(o domain.Owner) (owner, error) {
	oid, err := ids.ObjectID(o.UserID)
	if err != nil {
		return owner{}, err
	}
	return owner{UserID: oid, Name: o.Name, Email: o.Email, EmailVerified: o.EmailVerified}, nil
}

func (o owner) toDomain() domain.Owner {
	return domain.Owner{UserID: o.UserID.Hex(), Name: o.Name, Email: o.Email, EmailVerified: o.EmailVerified}
}

// ---- clients ----

type Clients struct{ coll *mongo.Collection }

var _ application.ClientRepository = (*Clients)(nil)

func NewClients(db *mongo.Database) *Clients { return &Clients{coll: db.Collection(ClientsCollection)} }

// DeleteUnused finds old clients that no grant or code names (clientId is
// a string: the hex ID, or a metadata document URL), then deletes them.
func (s *Clients) DeleteUnused(ctx context.Context, createdBefore time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	old := bson.M{"createdAt": bson.M{"$lt": createdBefore}}
	refs := func(from string) bson.D {
		return bson.D{{Key: "$lookup", Value: bson.M{
			"from": from, "localField": "idString", "foreignField": "clientId",
			"pipeline": bson.A{bson.M{"$limit": 1}, bson.M{"$project": bson.M{"_id": 1}}},
			"as":       from,
		}}}
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: old}},
		{{Key: "$sort", Value: bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}}},
		{{Key: "$addFields", Value: bson.M{"idString": bson.M{"$toString": "$_id"}}}},
		refs(GrantsCollection),
		refs(CodesCollection),
		{{Key: "$match", Value: bson.M{GrantsCollection: bson.A{}, CodesCollection: bson.A{}}}},
		{{Key: "$limit", Value: limit}},
		{{Key: "$project", Value: bson.M{"_id": 1}}},
	}
	cur, err := s.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, err
	}
	return deleteFound(ctx, s.coll, cur, old)
}

// deleteFound deletes the documents cur lists (by _id) that still match
// filter.
func deleteFound(ctx context.Context, coll *mongo.Collection, cur *mongo.Cursor, filter bson.M) (int64, error) {
	var found []struct {
		ID any `bson:"_id"`
	}
	if err := cur.All(ctx, &found); err != nil {
		return 0, err
	}
	if len(found) == 0 {
		return 0, nil
	}
	ids := make(bson.A, 0, len(found))
	for _, f := range found {
		ids = append(ids, f.ID)
	}
	res, err := coll.DeleteMany(ctx, bson.M{"$and": bson.A{bson.M{"_id": bson.M{"$in": ids}}, filter}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// deleteBatch deletes up to limit documents matching filter, in sortKey
// order.
func deleteBatch(ctx context.Context, coll *mongo.Collection, filter bson.M, sortKey string, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	opts := options.Find().SetSort(bson.D{{Key: sortKey, Value: 1}}).SetLimit(int64(limit)).
		SetProjection(bson.M{"_id": 1})
	cur, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return 0, err
	}
	return deleteFound(ctx, coll, cur, filter)
}

type clientDoc struct {
	ID           bson.ObjectID `bson:"_id"`
	Name         string        `bson:"name"`
	RedirectURIs []string      `bson:"redirectUris"`
	CreatedAt    time.Time     `bson:"createdAt"`
}

func (s *Clients) Create(ctx context.Context, c *domain.Client) error {
	doc := clientDoc{ID: bson.NewObjectID(), Name: c.Name, RedirectURIs: c.RedirectURIs, CreatedAt: c.CreatedAt}
	if _, err := s.coll.InsertOne(ctx, doc); err != nil {
		return err
	}
	c.ID = doc.ID.Hex()
	return nil
}

func (s *Clients) Get(ctx context.Context, id string) (*domain.Client, error) {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return nil, err
	}
	var d clientDoc
	if err := s.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&d); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	return &domain.Client{
		ID:           d.ID.Hex(),
		Name:         d.Name,
		RedirectURIs: slices.Clone(d.RedirectURIs),
		Kind:         domain.Registered,
		CreatedAt:    d.CreatedAt.UTC(),
	}, nil
}

// ---- codes ----

// Codes: one document per code hash (_id); a TTL index removes expired ones.
type Codes struct{ coll *mongo.Collection }

var _ application.CodeRepository = (*Codes)(nil)

func NewCodes(db *mongo.Database) *Codes { return &Codes{coll: db.Collection(CodesCollection)} }

type codeDoc struct {
	Hash        string    `bson:"_id"`
	ClientID    string    `bson:"clientId"`
	ClientName  string    `bson:"clientName"`
	RedirectURI string    `bson:"redirectUri"`
	Challenge   string    `bson:"challenge"`
	Resource    string    `bson:"resource"`
	Scope       string    `bson:"scope"`
	Owner       owner     `bson:",inline"`
	ExpiresAt   time.Time `bson:"expiresAt"`
	CreatedAt   time.Time `bson:"createdAt"`
}

func (s *Codes) Save(ctx context.Context, c *domain.Code) error {
	o, err := ownerDoc(c.Owner)
	if err != nil {
		return err
	}
	// The TTL index removes expired codes too, but only once a minute.
	if _, err := s.coll.DeleteMany(ctx, bson.M{"expiresAt": bson.M{"$lt": c.CreatedAt}}); err != nil {
		return err
	}
	_, err = s.coll.InsertOne(ctx, codeDoc{
		Hash: c.Hash, ClientID: c.ClientID, ClientName: c.ClientName, RedirectURI: c.RedirectURI,
		Challenge: c.Challenge, Resource: c.Resource, Scope: c.Scope, Owner: o,
		ExpiresAt: c.ExpiresAt, CreatedAt: c.CreatedAt,
	})
	return err
}

func (s *Codes) Consume(ctx context.Context, hash string) (*domain.Code, error) {
	var d codeDoc
	if err := s.coll.FindOneAndDelete(ctx, bson.M{"_id": hash}).Decode(&d); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	return &domain.Code{
		Hash: d.Hash, ClientID: d.ClientID, ClientName: d.ClientName, RedirectURI: d.RedirectURI,
		Challenge: d.Challenge, Resource: d.Resource, Scope: d.Scope, Owner: d.Owner.toDomain(),
		ExpiresAt: d.ExpiresAt.UTC(), CreatedAt: d.CreatedAt.UTC(),
	}, nil
}

func (s *Codes) DeleteForUser(ctx context.Context, userID string) error {
	oid, err := ids.ObjectID(userID)
	if err != nil {
		return err
	}
	_, err = s.coll.DeleteMany(ctx, bson.M{"userId": oid})
	return err
}

// DeleteExpired doesn't wait for the TTL index (which runs once a minute).
func (s *Codes) DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	return deleteBatch(ctx, s.coll, bson.M{"expiresAt": bson.M{"$lte": now}}, "expiresAt", limit)
}

// ---- grants ----

// Grants: a TTL index on refreshExpiresAt removes grants nobody can refresh.
type Grants struct{ coll *mongo.Collection }

var _ application.GrantRepository = (*Grants)(nil)

func NewGrants(db *mongo.Database) *Grants { return &Grants{coll: db.Collection(GrantsCollection)} }

type grantDoc struct {
	ID               bson.ObjectID `bson:"_id"`
	ClientID         string        `bson:"clientId"`
	ClientName       string        `bson:"clientName"`
	Scope            string        `bson:"scope"`
	Resource         string        `bson:"resource"`
	Owner            owner         `bson:",inline"`
	AccessHash       string        `bson:"accessHash"`
	AccessExpiresAt  time.Time     `bson:"accessExpiresAt"`
	RefreshHash      string        `bson:"refreshHash"`
	RefreshExpiresAt time.Time     `bson:"refreshExpiresAt"`
	CreatedAt        time.Time     `bson:"createdAt"`
	LastUsedAt       time.Time     `bson:"lastUsedAt"`
}

func (d grantDoc) toDomain() *domain.Grant {
	return &domain.Grant{
		ID: d.ID.Hex(), ClientID: d.ClientID, ClientName: d.ClientName, Scope: d.Scope, Resource: d.Resource,
		Owner:      d.Owner.toDomain(),
		AccessHash: d.AccessHash, AccessExpiresAt: d.AccessExpiresAt.UTC(),
		RefreshHash: d.RefreshHash, RefreshExpiresAt: d.RefreshExpiresAt.UTC(),
		CreatedAt: d.CreatedAt.UTC(), LastUsedAt: d.LastUsedAt.UTC(),
	}
}

func (s *Grants) Create(ctx context.Context, g *domain.Grant) error {
	o, err := ownerDoc(g.Owner)
	if err != nil {
		return err
	}
	expired := bson.M{"userId": o.UserID, "refreshExpiresAt": bson.M{"$lt": g.CreatedAt}}
	if _, err := s.coll.DeleteMany(ctx, expired); err != nil {
		return err
	}
	doc := grantDoc{
		ID: bson.NewObjectID(), ClientID: g.ClientID, ClientName: g.ClientName, Scope: g.Scope, Resource: g.Resource,
		Owner:      o,
		AccessHash: g.AccessHash, AccessExpiresAt: g.AccessExpiresAt,
		RefreshHash: g.RefreshHash, RefreshExpiresAt: g.RefreshExpiresAt,
		CreatedAt: g.CreatedAt, LastUsedAt: g.LastUsedAt,
	}
	if _, err := s.coll.InsertOne(ctx, doc); err != nil {
		return err
	}
	g.ID = doc.ID.Hex()
	return nil
}

func (s *Grants) Get(ctx context.Context, id string) (*domain.Grant, error) {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return nil, err
	}
	var d grantDoc
	if err := s.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&d); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	return d.toDomain(), nil
}

func (s *Grants) ListForUser(ctx context.Context, userID string) ([]*domain.Grant, error) {
	oid, err := ids.ObjectID(userID)
	if err != nil {
		return nil, err
	}
	sort := options.Find().SetSort(bson.D{{Key: "lastUsedAt", Value: -1}, {Key: "_id", Value: -1}})
	cur, err := s.coll.Find(ctx, bson.M{"userId": oid}, sort)
	if err != nil {
		return nil, err
	}
	var docs []grantDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]*domain.Grant, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toDomain())
	}
	return out, nil
}

func (s *Grants) Rotate(ctx context.Context, r application.Rotation) error {
	oid, err := ids.ObjectID(r.GrantID)
	if err != nil {
		return err
	}
	res, err := s.coll.UpdateOne(ctx, bson.M{"_id": oid, "refreshHash": r.RefreshHash}, bson.M{"$set": bson.M{
		"accessHash":       r.AccessHash,
		"accessExpiresAt":  r.AccessExpiresAt,
		"refreshHash":      r.NewRefreshHash,
		"refreshExpiresAt": r.RefreshExpiresAt,
		"lastUsedAt":       r.UsedAt,
	}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (s *Grants) Delete(ctx context.Context, userID, id string) error {
	oid, owner, err := ids.ObjectIDPair(id, userID)
	if err != nil {
		return err
	}
	return mongodb.RequireDeleted(s.coll.DeleteOne(ctx, bson.M{"_id": oid, "userId": owner}))
}

func (s *Grants) Revoke(ctx context.Context, id string) error {
	oid, err := ids.ObjectID(id)
	if errors.Is(err, apperrors.ErrInvalidID) {
		return nil // can't name a stored grant
	}
	if err != nil {
		return err
	}
	_, err = s.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

func (s *Grants) DeleteForUser(ctx context.Context, userID string) error {
	oid, err := ids.ObjectID(userID)
	if err != nil {
		return err
	}
	_, err = s.coll.DeleteMany(ctx, bson.M{"userId": oid})
	return err
}

// DeleteExpired doesn't wait for the TTL index (which runs once a minute).
func (s *Grants) DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	return deleteBatch(ctx, s.coll, bson.M{"refreshExpiresAt": bson.M{"$lte": now}}, "refreshExpiresAt", limit)
}
