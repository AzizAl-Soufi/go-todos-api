package todos

import (
	"context"
	"errors"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/mongodb"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// private struct implementing domain.TodosRepository
type mongoTodoRepo struct {
	coll *mongo.Collection
}

type mongoTodo struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	UserID    bson.ObjectID `bson:"userId"`
	Title     string        `bson:"title"`
	Completed bool          `bson:"completed"`
	CreatedAt time.Time     `bson:"createdAt,omitempty"`
}

func parseTodoID(id string) (bson.ObjectID, error) {
	parsed, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return bson.NilObjectID, apperrors.Validation("INVALID_ID", "invalid id format")
	}
	return parsed, nil
}

func toDomainTodo(todo *mongoTodo) *domain.Todo {
	return &domain.Todo{ID: todo.ID.Hex(), UserID: todo.UserID.Hex(), Title: todo.Title, Completed: todo.Completed, CreatedAt: todo.CreatedAt}
}

func NewMongoTodosRepository(db mongodb.MongoDBClient) TodosRepository {
	return &mongoTodoRepo{
		coll: db.Database().Collection("todos"),
	}
}

func (r *mongoTodoRepo) Create(ctx context.Context, userId string, todo *domain.Todo) error {
	userObjectID, err := parseTodoID(userId)
	if err != nil {
		return err
	}
	todoObjectID, err := parseTodoID(todo.ID)
	if err != nil {
		return err
	}
	stored := &mongoTodo{ID: todoObjectID, UserID: userObjectID, Title: todo.Title, Completed: todo.Completed, CreatedAt: todo.CreatedAt}

	_, err = r.coll.InsertOne(ctx, stored)
	return err
}

func (r *mongoTodoRepo) Update(ctx context.Context, id string, userId string, dto *domain.UpdateTodoDTO) error {
	idObjectID, err := parseTodoID(id)
	if err != nil {
		return err
	}
	userObjectID, err := parseTodoID(userId)
	if err != nil {
		return err
	}
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

	_, err = r.coll.UpdateOne(
		ctx,
		bson.M{"_id": idObjectID, "userId": userObjectID},
		bson.M{"$set": updateFields},
	)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return apperrors.ErrNotFound
		}

		return err
	}

	return nil
}

func (r *mongoTodoRepo) Get(ctx context.Context, id string, userId string) (*domain.Todo, error) {
	idObjectID, err := parseTodoID(id)
	if err != nil {
		return nil, err
	}
	userObjectID, err := parseTodoID(userId)
	if err != nil {
		return nil, err
	}
	var todo mongoTodo

	err = r.coll.FindOne(ctx, bson.M{"_id": idObjectID, "userId": userObjectID}).Decode(&todo)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, apperrors.ErrNotFound
		}
		return nil, err
	}

	return toDomainTodo(&todo), nil
}

func (r *mongoTodoRepo) Delete(ctx context.Context, todoId string, userId string) error {
	todoObjectID, err := parseTodoID(todoId)
	if err != nil {
		return err
	}
	userObjectID, err := parseTodoID(userId)
	if err != nil {
		return err
	}
	result, err := r.coll.DeleteOne(ctx, bson.M{"_id": todoObjectID, "userId": userObjectID})
	if err != nil {
		return err
	}

	if result.DeletedCount == 0 {
		return apperrors.ErrNotFound
	}

	return nil
}

func (r *mongoTodoRepo) GetAll(ctx context.Context, userId string) ([]*domain.Todo, error) {
	userObjectID, err := parseTodoID(userId)
	if err != nil {
		return nil, err
	}
	cursor, err := r.coll.Find(ctx, bson.M{"userId": userObjectID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var todos []*mongoTodo
	if err := cursor.All(ctx, &todos); err != nil {
		return nil, err
	}

	if todos == nil {
		return []*domain.Todo{}, nil
	}

	domainTodos := make([]*domain.Todo, 0, len(todos))
	for _, todo := range todos {
		domainTodos = append(domainTodos, toDomainTodo(todo))
	}
	return domainTodos, nil
}

func (r *mongoTodoRepo) DeleteByUserID(ctx context.Context, userId string) error {
	userObjectID, err := parseTodoID(userId)
	if err != nil {
		return err
	}
	result, err := r.coll.DeleteMany(ctx, bson.M{"userId": userObjectID})
	if err != nil {
		return err
	}

	if result.DeletedCount == 0 {
		return apperrors.ErrNotFound
	}

	return nil
}
