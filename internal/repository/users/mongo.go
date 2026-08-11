package users

import (
	"context"
	"errors"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// private struct implementing domain.TodosRepository
type mongoUserRepo struct {
	coll *mongo.Collection
}

type mongoUser struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	Name      string        `bson:"name"`
	Email     string        `bson:"email"`
	CreatedAt time.Time     `bson:"createdAt,omitempty"`
}

func parseUserID(id string) (bson.ObjectID, error) {
	parsed, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return bson.NilObjectID, apperrors.Validation("INVALID_ID", "invalid id format")
	}
	return parsed, nil
}

func NewMongoUsersRepository(db mongodb.MongoDBClient) UsersRepository {
	return &mongoUserRepo{
		coll: db.Database().Collection("users"),
	}
}

func (r *mongoUserRepo) getUser(ctx context.Context, field string, value any) (*domain.User, error) {
	var userData mongoUser

	exists := r.coll.FindOne(ctx, bson.M{field: value})
	if err := exists.Err(); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.ErrNotFound
		}
		return nil, err
	}

	if err := exists.Decode(&userData); err != nil {
		return nil, err
	}
	return &domain.User{ID: userData.ID.Hex(), Name: userData.Name, Email: userData.Email, CreatedAt: userData.CreatedAt, IsNew: false}, nil
}

func (r *mongoUserRepo) Register(ctx context.Context, user *domain.UserDTO) (*domain.User, error) {
	var userData *domain.User

	existsUser, err := r.getUser(ctx, "email", user.Email)
	if err != nil {
		if _, ok := apperrors.From(err); ok {
			newUser := domain.User{
				Name:      user.Name,
				Email:     user.Email,
				CreatedAt: time.Now().UTC(),
				IsNew:     true,
			}
			result, err := r.coll.InsertOne(ctx, bson.M{"name": newUser.Name, "email": newUser.Email, "createdAt": newUser.CreatedAt})
			if err != nil {
				return nil, err
			}
			newUser.ID = result.InsertedID.(bson.ObjectID).Hex()
			userData = &newUser
			return userData, nil
		} else {
			return nil, err
		}
	}
	userData = existsUser

	return userData, nil
}

func (r *mongoUserRepo) GetByID(ctx context.Context, id string) (*domain.User, error) {
	parsed, err := parseUserID(id)
	if err != nil {
		return nil, err
	}
	return r.getUser(ctx, "_id", parsed)
}

func (r *mongoUserRepo) Auth(ctx context.Context, id string) (*domain.User, error) {
	parsed, err := parseUserID(id)
	if err != nil {
		return nil, err
	}
	return r.getUser(ctx, "_id", parsed)
}

func (r *mongoUserRepo) GetOverview(ctx context.Context, id string) (*domain.User, error) {
	parsed, err := parseUserID(id)
	if err != nil {
		return nil, err
	}
	return r.getUser(ctx, "_id", parsed)
}

func (r *mongoUserRepo) Delete(ctx context.Context, id string) error {
	parsed, err := parseUserID(id)
	if err != nil {
		return err
	}
	result, err := r.coll.DeleteOne(ctx, bson.M{"_id": parsed})
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return apperrors.ErrNotFound
		}

		return err
	}

	if result.DeletedCount == 0 {
		return apperrors.ErrNotFound
	}

	return nil
}
