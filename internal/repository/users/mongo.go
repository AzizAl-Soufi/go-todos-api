package users

import (
	"context"
	"errors"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/shared/ids"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type mongoUserRepo struct {
	coll *mongo.Collection
}

type mongoUser struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	Name      string        `bson:"name"`
	Email     string        `bson:"email"`
	CreatedAt time.Time     `bson:"createdAt,omitempty"`
}

func NewMongoUsersRepository(db mongodb.MongoDBClient) UsersRepository {
	return &mongoUserRepo{
		coll: db.Database().Collection("users"),
	}
}

func (r *mongoUserRepo) findOne(ctx context.Context, filter bson.M) (*domain.User, error) {
	var u mongoUser
	if err := r.coll.FindOne(ctx, filter).Decode(&u); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	return &domain.User{ID: u.ID.Hex(), Name: u.Name, Email: u.Email, CreatedAt: u.CreatedAt}, nil
}

func (r *mongoUserRepo) findByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.findOne(ctx, bson.M{"email": email})
}

func (r *mongoUserRepo) findByID(ctx context.Context, id string) (*domain.User, error) {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return nil, err
	}
	return r.findOne(ctx, bson.M{"_id": oid})
}

func (r *mongoUserRepo) Register(ctx context.Context, user *domain.UserDTO) (*domain.User, error) {
	existing, err := r.findByEmail(ctx, user.Email)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return nil, err
	}

	newUser := domain.User{
		Name:      user.Name,
		Email:     user.Email,
		CreatedAt: time.Now().UTC(),
		IsNew:     true,
	}
	result, err := r.coll.InsertOne(ctx, bson.M{"name": newUser.Name, "email": newUser.Email, "createdAt": newUser.CreatedAt})
	if err != nil {
		// A concurrent registration with the same email won the race; the
		// unique email index rejects ours, so return the user it created.
		if mongo.IsDuplicateKeyError(err) {
			return r.findByEmail(ctx, user.Email)
		}
		return nil, err
	}

	oid, ok := result.InsertedID.(bson.ObjectID)
	if !ok {
		return nil, apperrors.Internal("UNEXPECTED_ID", "unexpected inserted id type")
	}
	newUser.ID = oid.Hex()
	return &newUser, nil
}

func (r *mongoUserRepo) GetByID(ctx context.Context, id string) (*domain.User, error) {
	return r.findByID(ctx, id)
}

func (r *mongoUserRepo) Auth(ctx context.Context, id string) (*domain.User, error) {
	return r.findByID(ctx, id)
}

func (r *mongoUserRepo) GetOverview(ctx context.Context, id string) (*domain.User, error) {
	return r.findByID(ctx, id)
}

func (r *mongoUserRepo) Delete(ctx context.Context, id string) error {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return err
	}
	return mongodb.RequireDeleted(r.coll.DeleteOne(ctx, bson.M{"_id": oid}))
}
