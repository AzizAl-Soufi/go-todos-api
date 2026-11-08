// Package mongostore holds the MongoDB identity repositories. Calls made with
// a transactional context (platform/database/mongodb.TxRunner) join it.
package mongostore

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
)

const (
	UsersCollection      = "users"
	PasskeysCollection   = "passkeys"
	ChallengesCollection = "passkey_challenges"
)

// ---- users ----

type Users struct{ coll *mongo.Collection }

var _ application.UserRepository = (*Users)(nil)

func NewUsers(db *mongo.Database) *Users { return &Users{coll: db.Collection(UsersCollection)} }

type userDoc struct {
	ID                 bson.ObjectID `bson:"_id,omitempty"`
	Name               string        `bson:"name"`
	Email              string        `bson:"email"`
	FirebaseUID        string        `bson:"firebaseUid,omitempty"`
	EmailVerifiedAt    *time.Time    `bson:"emailVerifiedAt,omitempty"`
	SessionsValidAfter *time.Time    `bson:"sessionsValidAfter,omitempty"`
	CreatedAt          time.Time     `bson:"createdAt"`
}

func (d *userDoc) toDomain() *domain.User {
	return &domain.User{
		ID: d.ID.Hex(), Name: d.Name, Email: d.Email, FirebaseUID: d.FirebaseUID,
		EmailVerifiedAt: d.EmailVerifiedAt, SessionsValidAfter: d.SessionsValidAfter, CreatedAt: d.CreatedAt,
	}
}

// duplicateKey maps a duplicate key error to the port error of its index
// (created by the migrations).
func duplicateKey(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return nil
	}
	if strings.Contains(err.Error(), "uniq_firebase_uid") {
		return domain.ErrFirebaseUIDTaken
	}
	return domain.ErrEmailTaken
}

func (s *Users) Create(ctx context.Context, u *domain.User) error {
	doc := userDoc{
		ID: bson.NewObjectID(), Name: u.Name, Email: u.Email, FirebaseUID: u.FirebaseUID,
		EmailVerifiedAt: u.EmailVerifiedAt, SessionsValidAfter: u.SessionsValidAfter, CreatedAt: u.CreatedAt,
	}
	if _, err := s.coll.InsertOne(ctx, doc); err != nil {
		if mapped := duplicateKey(err); mapped != nil {
			return mapped
		}
		return err
	}
	u.ID = doc.ID.Hex()
	return nil
}

func (s *Users) findOne(ctx context.Context, filter bson.M) (*domain.User, error) {
	var doc userDoc
	if err := s.coll.FindOne(ctx, filter).Decode(&doc); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	return doc.toDomain(), nil
}

func (s *Users) FindByID(ctx context.Context, id string) (*domain.User, error) {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return nil, err
	}
	return s.findOne(ctx, bson.M{"_id": oid})
}

func (s *Users) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	return s.findOne(ctx, bson.M{"email": email})
}

func (s *Users) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*domain.User, error) {
	if firebaseUID == "" {
		return nil, apperrors.ErrNotFound
	}
	return s.findOne(ctx, bson.M{"firebaseUid": firebaseUID})
}

func (s *Users) updateOne(ctx context.Context, id string, filter, update bson.M) error {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return err
	}
	filter["_id"] = oid
	res, err := s.coll.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (s *Users) LinkFirebaseUID(ctx context.Context, id, firebaseUID string) error {
	err := s.updateOne(ctx, id, bson.M{"firebaseUid": bson.M{"$exists": false}}, bson.M{"$set": bson.M{"firebaseUid": firebaseUID}})
	if mapped := duplicateKey(err); mapped != nil {
		return mapped
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		if _, err := s.FindByID(ctx, id); err != nil {
			return err
		}
		return domain.ErrFirebaseUIDTaken // the account is linked already
	}
	return err
}

func (s *Users) MarkEmailVerified(ctx context.Context, id string, at time.Time) error {
	// Keep the first verification time: only set the field when it is unset.
	err := s.updateOne(ctx, id, bson.M{"emailVerifiedAt": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"emailVerifiedAt": at}})
	if errors.Is(err, apperrors.ErrNotFound) {
		_, err = s.FindByID(ctx, id) // already verified is fine; missing user is not
	}
	return err
}

func (s *Users) RevokeSessions(ctx context.Context, id string, at time.Time) error {
	return s.updateOne(ctx, id, bson.M{}, bson.M{"$set": bson.M{"sessionsValidAfter": at}})
}

func (s *Users) Delete(ctx context.Context, id string) error {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return err
	}
	return mongodb.RequireDeleted(s.coll.DeleteOne(ctx, bson.M{"_id": oid}))
}

// ---- passkeys ----

type Passkeys struct{ coll *mongo.Collection }

var _ application.PasskeyRepository = (*Passkeys)(nil)

func NewPasskeys(db *mongo.Database) *Passkeys {
	return &Passkeys{coll: db.Collection(PasskeysCollection)}
}

type passkeyDoc struct {
	ID           bson.ObjectID `bson:"_id"`
	UserID       bson.ObjectID `bson:"userId"`
	Name         string        `bson:"name"`
	CredentialID []byte        `bson:"credentialId"`
	Credential   []byte        `bson:"credential"`
	CreatedAt    time.Time     `bson:"createdAt"`
	LastUsedAt   *time.Time    `bson:"lastUsedAt,omitempty"`
}

func (s *Passkeys) Create(ctx context.Context, p *domain.Passkey) error {
	userOID, err := ids.ObjectID(p.UserID)
	if err != nil {
		return err
	}
	doc := passkeyDoc{
		ID: bson.NewObjectID(), UserID: userOID, Name: p.Name, CredentialID: p.CredentialID,
		Credential: p.Credential, CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt,
	}
	if _, err := s.coll.InsertOne(ctx, doc); err != nil {
		// Relies on the unique credentialId index created by the migrations.
		if mongo.IsDuplicateKeyError(err) {
			return domain.ErrPasskeyExists
		}
		return err
	}
	p.ID = doc.ID.Hex()
	return nil
}

func (s *Passkeys) ListForUser(ctx context.Context, userID string) ([]*domain.Passkey, error) {
	oid, err := ids.ObjectID(userID)
	if err != nil {
		return nil, err
	}
	cur, err := s.coll.Find(ctx, bson.M{"userId": oid}, options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var docs []passkeyDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]*domain.Passkey, 0, len(docs))
	for _, d := range docs {
		out = append(out, &domain.Passkey{
			ID: d.ID.Hex(), UserID: d.UserID.Hex(), Name: d.Name, CredentialID: d.CredentialID,
			Credential: d.Credential, CreatedAt: d.CreatedAt, LastUsedAt: d.LastUsedAt,
		})
	}
	return out, nil
}

func (s *Passkeys) RecordUse(ctx context.Context, id string, credential []byte, at time.Time) error {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return err
	}
	res, err := s.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{"credential": credential, "lastUsedAt": at}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (s *Passkeys) Delete(ctx context.Context, userID, id string) error {
	userOID, err := ids.ObjectID(userID)
	if err != nil {
		return err
	}
	oid, err := ids.ObjectID(id)
	if err != nil {
		return err
	}
	return mongodb.RequireDeleted(s.coll.DeleteOne(ctx, bson.M{"_id": oid, "userId": userOID}))
}

func (s *Passkeys) DeleteAllForUser(ctx context.Context, userID string) error {
	oid, err := ids.ObjectID(userID)
	if err != nil {
		return err
	}
	_, err = s.coll.DeleteMany(ctx, bson.M{"userId": oid})
	return err
}

// ---- passkey challenges (_id = token hash) ----

type PasskeyChallenges struct{ coll *mongo.Collection }

var _ application.PasskeyChallengeRepository = (*PasskeyChallenges)(nil)

func NewPasskeyChallenges(db *mongo.Database) *PasskeyChallenges {
	return &PasskeyChallenges{coll: db.Collection(ChallengesCollection)}
}

type challengeDoc struct {
	TokenHash string         `bson:"_id"`
	UserID    *bson.ObjectID `bson:"userId,omitempty"`
	State     []byte         `bson:"state"`
	ExpiresAt time.Time      `bson:"expiresAt"`
	CreatedAt time.Time      `bson:"createdAt"`
}

func (s *PasskeyChallenges) Save(ctx context.Context, c *domain.PasskeyChallenge) error {
	doc := challengeDoc{TokenHash: c.TokenHash, State: c.State, ExpiresAt: c.ExpiresAt, CreatedAt: c.CreatedAt}
	if c.UserID != "" {
		oid, err := ids.ObjectID(c.UserID)
		if err != nil {
			return err
		}
		doc.UserID = &oid
	}
	// The TTL index removes expired challenges too, but only once a minute.
	if _, err := s.coll.DeleteMany(ctx, bson.M{"expiresAt": bson.M{"$lt": c.CreatedAt}}); err != nil {
		return err
	}
	_, err := s.coll.InsertOne(ctx, doc)
	return err
}

func (s *PasskeyChallenges) Consume(ctx context.Context, tokenHash string) (*domain.PasskeyChallenge, error) {
	var doc challengeDoc
	if err := s.coll.FindOneAndDelete(ctx, bson.M{"_id": tokenHash}).Decode(&doc); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	c := &domain.PasskeyChallenge{TokenHash: doc.TokenHash, State: doc.State, ExpiresAt: doc.ExpiresAt, CreatedAt: doc.CreatedAt}
	if doc.UserID != nil {
		c.UserID = doc.UserID.Hex()
	}
	return c, nil
}

func (s *PasskeyChallenges) DeleteForUser(ctx context.Context, userID string) error {
	oid, err := ids.ObjectID(userID)
	if err != nil {
		return err
	}
	_, err = s.coll.DeleteMany(ctx, bson.M{"userId": oid})
	return err
}
