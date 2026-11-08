// Package mongostore holds the MongoDB teams, members and invitations
// repositories. Calls made with a transactional context join it.
package mongostore

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
)

const (
	TeamsCollection       = "teams"
	MembersCollection     = "team_members"
	InvitationsCollection = "team_invitations"
)

func requireMatched(res *mongo.UpdateResult, err error) error {
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func findAll[T any](ctx context.Context, coll *mongo.Collection, filter bson.M, sort bson.D) ([]T, error) {
	cursor, err := coll.Find(ctx, filter, options.Find().SetSort(sort))
	if err != nil {
		return nil, err
	}
	var docs []T
	err = cursor.All(ctx, &docs)
	return docs, err
}

// ---- teams ----

type Teams struct{ coll *mongo.Collection }

var _ application.TeamRepository = (*Teams)(nil)

func NewTeams(db *mongo.Database) *Teams { return &Teams{coll: db.Collection(TeamsCollection)} }

type teamDoc struct {
	ID        bson.ObjectID `bson:"_id"`
	Name      string        `bson:"name"`
	CreatedAt time.Time     `bson:"createdAt"`
	UpdatedAt time.Time     `bson:"updatedAt"`
}

func (s *Teams) Create(ctx context.Context, t *domain.Team) error {
	doc := teamDoc{ID: bson.NewObjectID(), Name: t.Name, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt}
	if _, err := s.coll.InsertOne(ctx, doc); err != nil {
		return err
	}
	t.ID = doc.ID.Hex()
	return nil
}

func (s *Teams) Get(ctx context.Context, id string) (*domain.Team, error) {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return nil, err
	}
	var d teamDoc
	if err := s.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&d); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	return &domain.Team{ID: d.ID.Hex(), Name: d.Name, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC()}, nil
}

func (s *Teams) Update(ctx context.Context, t *domain.Team) error {
	oid, err := ids.ObjectID(t.ID)
	if err != nil {
		return err
	}
	return requireMatched(s.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{"name": t.Name, "updatedAt": t.UpdatedAt}}))
}

func (s *Teams) Delete(ctx context.Context, id string) error {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return err
	}
	return mongodb.RequireDeleted(s.coll.DeleteOne(ctx, bson.M{"_id": oid}))
}

// ---- members ----

type Members struct{ coll *mongo.Collection }

var _ application.MemberRepository = (*Members)(nil)

func NewMembers(db *mongo.Database) *Members { return &Members{coll: db.Collection(MembersCollection)} }

type memberDoc struct {
	ID       bson.ObjectID `bson:"_id"`
	TeamID   bson.ObjectID `bson:"teamId"`
	UserID   bson.ObjectID `bson:"userId"`
	Name     string        `bson:"name"`
	Email    string        `bson:"email"`
	Role     string        `bson:"role"`
	JoinedAt time.Time     `bson:"joinedAt"`
}

func (d memberDoc) toDomain() *domain.Member {
	return &domain.Member{
		TeamID: d.TeamID.Hex(), UserID: d.UserID.Hex(), Name: d.Name, Email: d.Email,
		Role: access.Role(d.Role), JoinedAt: d.JoinedAt.UTC(),
	}
}

func memberKey(teamID, userID string) (bson.M, error) {
	team, user, err := ids.ObjectIDPair(teamID, userID)
	if err != nil {
		return nil, err
	}
	return bson.M{"teamId": team, "userId": user}, nil
}

var byJoined = bson.D{{Key: "joinedAt", Value: 1}, {Key: "teamId", Value: 1}, {Key: "userId", Value: 1}}

func (s *Members) Add(ctx context.Context, m *domain.Member) error {
	team, user, err := ids.ObjectIDPair(m.TeamID, m.UserID)
	if err != nil {
		return err
	}
	_, err = s.coll.InsertOne(ctx, memberDoc{
		ID: bson.NewObjectID(), TeamID: team, UserID: user, Name: m.Name, Email: m.Email, Role: string(m.Role), JoinedAt: m.JoinedAt,
	})
	if mongo.IsDuplicateKeyError(err) {
		return apperrors.ErrDuplicate
	}
	return err
}

func (s *Members) Get(ctx context.Context, teamID, userID string) (*domain.Member, error) {
	key, err := memberKey(teamID, userID)
	if err != nil {
		return nil, err
	}
	var d memberDoc
	if err := s.coll.FindOne(ctx, key).Decode(&d); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	return d.toDomain(), nil
}

func (s *Members) SetRole(ctx context.Context, teamID, userID string, role access.Role) error {
	key, err := memberKey(teamID, userID)
	if err != nil {
		return err
	}
	return requireMatched(s.coll.UpdateOne(ctx, key, bson.M{"$set": bson.M{"role": string(role)}}))
}

func (s *Members) Remove(ctx context.Context, teamID, userID string) error {
	key, err := memberKey(teamID, userID)
	if err != nil {
		return err
	}
	return mongodb.RequireDeleted(s.coll.DeleteOne(ctx, key))
}

func (s *Members) listBy(ctx context.Context, field, id string) ([]*domain.Member, error) {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return nil, err
	}
	docs, err := findAll[memberDoc](ctx, s.coll, bson.M{field: oid}, byJoined)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Member, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toDomain())
	}
	return out, nil
}

func (s *Members) ListByTeam(ctx context.Context, teamID string) ([]*domain.Member, error) {
	return s.listBy(ctx, "teamId", teamID)
}

func (s *Members) ListByUser(ctx context.Context, userID string) ([]*domain.Member, error) {
	return s.listBy(ctx, "userId", userID)
}

func (s *Members) DeleteByTeam(ctx context.Context, teamID string) error {
	oid, err := ids.ObjectID(teamID)
	if err != nil {
		return err
	}
	_, err = s.coll.DeleteMany(ctx, bson.M{"teamId": oid})
	return err
}

// ---- invitations ----

type Invitations struct{ coll *mongo.Collection }

var _ application.InvitationRepository = (*Invitations)(nil)

func NewInvitations(db *mongo.Database) *Invitations {
	return &Invitations{coll: db.Collection(InvitationsCollection)}
}

type invitationDoc struct {
	ID        bson.ObjectID `bson:"_id"`
	TeamID    bson.ObjectID `bson:"teamId"`
	Email     string        `bson:"email"`
	Role      string        `bson:"role"`
	InvitedBy bson.ObjectID `bson:"invitedBy"`
	TokenHash string        `bson:"tokenHash"`
	ExpiresAt time.Time     `bson:"expiresAt"`
	CreatedAt time.Time     `bson:"createdAt"`
	SentAt    time.Time     `bson:"sentAt"`
}

func (d invitationDoc) toDomain() *domain.Invitation {
	sentAt := d.SentAt
	if sentAt.IsZero() { // written by a release from before sentAt (migration 009)
		sentAt = d.CreatedAt
	}
	return &domain.Invitation{
		ID: d.ID.Hex(), TeamID: d.TeamID.Hex(), Email: d.Email, Role: access.Role(d.Role), InvitedBy: d.InvitedBy.Hex(),
		TokenHash: d.TokenHash, ExpiresAt: d.ExpiresAt.UTC(), CreatedAt: d.CreatedAt.UTC(), SentAt: sentAt.UTC(),
	}
}

func (s *Invitations) Save(ctx context.Context, inv *domain.Invitation) error {
	team, invitedBy, err := ids.ObjectIDPair(inv.TeamID, inv.InvitedBy)
	if err != nil {
		return err
	}
	doc := invitationDoc{
		ID: bson.NewObjectID(), TeamID: team, Email: inv.Email, Role: string(inv.Role), InvitedBy: invitedBy,
		TokenHash: inv.TokenHash, ExpiresAt: inv.ExpiresAt, CreatedAt: inv.CreatedAt, SentAt: inv.SentAt,
	}
	// Replace the pending invitation for this team and email (new ID and token).
	if _, err := s.coll.DeleteOne(ctx, bson.M{"teamId": team, "email": inv.Email}); err != nil {
		return err
	}
	if _, err := s.coll.InsertOne(ctx, doc); err != nil {
		return err
	}
	inv.ID = doc.ID.Hex()
	return nil
}

func (s *Invitations) findOne(ctx context.Context, filter bson.M) (*domain.Invitation, error) {
	var d invitationDoc
	if err := s.coll.FindOne(ctx, filter).Decode(&d); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	return d.toDomain(), nil
}

func byTeamAndID(teamID, id string) (bson.M, error) {
	oid, team, err := ids.ObjectIDPair(id, teamID)
	if err != nil {
		return nil, err
	}
	return bson.M{"_id": oid, "teamId": team}, nil
}

func (s *Invitations) Get(ctx context.Context, teamID, id string) (*domain.Invitation, error) {
	filter, err := byTeamAndID(teamID, id)
	if err != nil {
		return nil, err
	}
	return s.findOne(ctx, filter)
}

func (s *Invitations) FindByTokenHash(ctx context.Context, tokenHash string) (*domain.Invitation, error) {
	return s.findOne(ctx, bson.M{"tokenHash": tokenHash})
}

func (s *Invitations) Resend(ctx context.Context, r application.InvitationResend) error {
	filter, err := byTeamAndID(r.TeamID, r.ID)
	if err != nil {
		return err
	}
	filter["$or"] = bson.A{
		bson.M{"sentAt": bson.M{"$lte": r.SentBefore}},
		bson.M{"sentAt": bson.M{"$exists": false}}, // see toDomain
	}
	res, err := s.coll.UpdateOne(ctx, filter, bson.M{"$set": bson.M{
		"tokenHash": r.TokenHash,
		"expiresAt": r.ExpiresAt,
		"sentAt":    r.SentAt,
	}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (s *Invitations) Delete(ctx context.Context, teamID, id string) error {
	filter, err := byTeamAndID(teamID, id)
	if err != nil {
		return err
	}
	return mongodb.RequireDeleted(s.coll.DeleteOne(ctx, filter))
}

func (s *Invitations) ListByTeam(ctx context.Context, teamID string) ([]*domain.Invitation, error) {
	oid, err := ids.ObjectID(teamID)
	if err != nil {
		return nil, err
	}
	docs, err := findAll[invitationDoc](ctx, s.coll, bson.M{"teamId": oid}, bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}})
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Invitation, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toDomain())
	}
	return out, nil
}

func (s *Invitations) DeleteExpired(ctx context.Context, before time.Time, limit int) (int64, error) {
	// Bounded like the other stores: look up a batch of IDs, delete those.
	opts := options.Find().SetProjection(bson.M{"_id": 1}).SetSort(bson.D{{Key: "expiresAt", Value: 1}}).SetLimit(int64(limit))
	cur, err := s.coll.Find(ctx, bson.M{"expiresAt": bson.M{"$lt": before}}, opts)
	if err != nil {
		return 0, err
	}
	var docs []struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err := cur.All(ctx, &docs); err != nil || len(docs) == 0 {
		return 0, err
	}
	batch := make([]bson.ObjectID, len(docs))
	for i, d := range docs {
		batch[i] = d.ID
	}
	res, err := s.coll.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": batch}})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func (s *Invitations) DeleteByTeam(ctx context.Context, teamID string) error {
	oid, err := ids.ObjectID(teamID)
	if err != nil {
		return err
	}
	_, err = s.coll.DeleteMany(ctx, bson.M{"teamId": oid})
	return err
}
