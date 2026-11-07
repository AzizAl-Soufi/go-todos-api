package todos

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/shared/ids"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

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

func toDomainTodo(todo *mongoTodo) *domain.Todo {
	return &domain.Todo{ID: todo.ID.Hex(), UserID: todo.UserID.Hex(), Title: todo.Title, Completed: todo.Completed, CreatedAt: todo.CreatedAt}
}

// ownedBy filters a single todo by its ID and owner.
func ownedBy(id, userID bson.ObjectID) bson.M {
	return bson.M{"_id": id, "userId": userID}
}

func NewMongoTodosRepository(db mongodb.MongoDBClient) TodosRepository {
	return &mongoTodoRepo{
		coll: db.Database().Collection("todos"),
	}
}

func (r *mongoTodoRepo) Create(ctx context.Context, userID string, todo *domain.Todo) error {
	todoOID, userOID, err := ids.ObjectIDPair(todo.ID, userID)
	if err != nil {
		return err
	}

	_, err = r.coll.InsertOne(ctx, &mongoTodo{
		ID:        todoOID,
		UserID:    userOID,
		Title:     todo.Title,
		Completed: todo.Completed,
		CreatedAt: todo.CreatedAt,
	})
	return err
}

func (r *mongoTodoRepo) Update(ctx context.Context, id, userID string, dto *domain.UpdateTodoDTO) error {
	todoOID, userOID, err := ids.ObjectIDPair(id, userID)
	if err != nil {
		return err
	}

	set := bson.M{}
	if dto.Title != nil {
		set["title"] = *dto.Title
	}
	if dto.Completed != nil {
		set["completed"] = *dto.Completed
	}
	if len(set) == 0 {
		return nil
	}

	_, err = r.coll.UpdateOne(ctx, ownedBy(todoOID, userOID), bson.M{"$set": set})
	return mongodb.MapNotFound(err)
}

func (r *mongoTodoRepo) Get(ctx context.Context, id, userID string) (*domain.Todo, error) {
	todoOID, userOID, err := ids.ObjectIDPair(id, userID)
	if err != nil {
		return nil, err
	}

	var todo mongoTodo
	if err := r.coll.FindOne(ctx, ownedBy(todoOID, userOID)).Decode(&todo); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	return toDomainTodo(&todo), nil
}

func (r *mongoTodoRepo) Delete(ctx context.Context, id, userID string) error {
	todoOID, userOID, err := ids.ObjectIDPair(id, userID)
	if err != nil {
		return err
	}
	return mongodb.RequireDeleted(r.coll.DeleteOne(ctx, ownedBy(todoOID, userOID)))
}

func (r *mongoTodoRepo) GetAll(ctx context.Context, userID string) ([]*domain.Todo, error) {
	userOID, err := ids.ObjectID(userID)
	if err != nil {
		return nil, err
	}

	cursor, err := r.coll.Find(ctx, bson.M{"userId": userOID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var todos []*mongoTodo
	if err := cursor.All(ctx, &todos); err != nil {
		return nil, err
	}

	result := make([]*domain.Todo, 0, len(todos))
	for _, todo := range todos {
		result = append(result, toDomainTodo(todo))
	}
	return result, nil
}

func (r *mongoTodoRepo) DeleteByUserID(ctx context.Context, userID string) error {
	userOID, err := ids.ObjectID(userID)
	if err != nil {
		return err
	}
	return mongodb.RequireDeleted(r.coll.DeleteMany(ctx, bson.M{"userId": userOID}))
}
