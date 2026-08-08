package repository

import (
	"context"
	"errors"

	"github.com/AzizAl-Soufi/todos-api/internal/pkg/database/mongodb"
	"github.com/AzizAl-Soufi/todos-api/internal/todos/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// private struct implementing domain.TodosRepository
type mongoTodoRepo struct {
	coll *mongo.Collection
}

func NewMongoTodosRepository(db mongodb.MongoDBClient) TodosRepository {
	return &mongoTodoRepo{
		coll: db.Database().Collection("todos"),
	}
}

func (r *mongoTodoRepo) Create(ctx context.Context, todo *domain.Todo) error {
	if todo.ID == (bson.ObjectID{}) {
		todo.ID = bson.NewObjectID()
	}

	_, err := r.coll.InsertOne(ctx, todo)
	return err
}

func (r *mongoTodoRepo) Update(ctx context.Context, id bson.ObjectID, dto *domain.UpdateTodoDTO) error {
	updateFields := bson.M{}

	if dto.Title != nil {
		updateFields["title"] = *dto.Title
	}

	if dto.Completed != nil {
		updateFields["completed"] = *dto.Completed
	}

	if len(updateFields) == 0 {
		return nil
	}

	_, err := r.coll.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{"$set": updateFields},
	)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrNotFound
		}

		return err
	}

	return nil
}

func (r *mongoTodoRepo) GetAll(ctx context.Context) ([]*domain.Todo, error) {
	cursor, err := r.coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var todos []*domain.Todo
	if err := cursor.All(ctx, &todos); err != nil {
		return nil, err
	}

	if todos == nil {
		return []*domain.Todo{}, nil
	}

	return todos, nil
}

func (r *mongoTodoRepo) GetByID(ctx context.Context, id bson.ObjectID) (*domain.Todo, error) {
	var todo domain.Todo

	err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&todo)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &todo, nil
}

func (r *mongoTodoRepo) DeleteByID(ctx context.Context, id bson.ObjectID) error {
	result, err := r.coll.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}

	if result.DeletedCount == 0 {
		return ErrNotFound
	}

	return nil
}
