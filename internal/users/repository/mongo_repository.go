package repository

import (
	"context"
	"errors"

	"github.com/AzizAl-Soufi/todos-api/internal/common"
	"github.com/AzizAl-Soufi/todos-api/internal/pkg/database/mongodb"
	"github.com/AzizAl-Soufi/todos-api/internal/users/domain"
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

func (r *mongoUserRepo) Auth(ctx context.Context, user *domain.User) (*domain.User, error) {
	var userData domain.User

	exists := r.coll.FindOne(ctx, bson.M{"email": user.Email})
	if err := exists.Err(); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			result, err := r.coll.InsertOne(ctx, user)
			if err != nil {
				return nil, err
			}
			u := r.coll.FindOne(ctx, bson.M{"_id": result.InsertedID})
			u.Decode(&userData)
			userData.IsNew = true
			return &userData, nil
		} else {
			return nil, err
		}
	} else {
		exists.Decode(&userData)
		userData.IsNew = false
		return &userData, nil
	}
}

func (r *mongoUserRepo) GetOverview(ctx context.Context, id bson.ObjectID) (*domain.User, error) {
	cursor := r.coll.FindOne(ctx, bson.M{"_id": id})
	if err := cursor.Err(); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, common.ErrNotFound
		}
		return nil, cursor.Err()
	}

	var user *domain.User
	if err := cursor.Decode(&user); err != nil {
		return nil, err
	}

	return user, nil
}

func (r *mongoUserRepo) Delete(ctx context.Context, id bson.ObjectID) error {
	result, err := r.coll.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}

	if result.DeletedCount == 0 {
		return common.ErrNotFound
	}

	return nil
}
