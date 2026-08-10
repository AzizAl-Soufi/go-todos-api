package repository

import (
	"context"
	"errors"
	"time"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/common/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/pkg/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/users/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// private struct implementing domain.TodosRepository
type mongoUserRepo struct {
	coll *mongo.Collection
}

func NewMongoUsersRepository(db mongodb.MongoDBClient) UsersRepository {
	return &mongoUserRepo{
		coll: db.Database().Collection("users"),
	}
}

func (r *mongoUserRepo) getUser(ctx context.Context, field string, value any) (*domain.User, error) {
	var userData *domain.User

	exists := r.coll.FindOne(ctx, bson.M{field: value})
	if err := exists.Err(); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.ErrNotFound
		}
		return nil, err
	}

	exists.Decode(&userData)
	userData.IsNew = false
	return userData, nil
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
			result, err := r.coll.InsertOne(ctx, newUser)
			if err != nil {
				return nil, err
			}
			newUser.ID = result.InsertedID.(bson.ObjectID)
			userData = &newUser
			return userData, nil
		} else {
			return nil, err
		}
	}
	userData = existsUser

	return userData, nil
}

func (r *mongoUserRepo) GetByID(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	return r.getUser(ctx, "_id", id)
}

func (r *mongoUserRepo) Auth(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	return r.getUser(ctx, "_id", id)
}

func (r *mongoUserRepo) GetOverview(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	return r.getUser(ctx, "_id", id)
}

func (r *mongoUserRepo) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.coll.DeleteOne(ctx, bson.M{"_id": id})
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
