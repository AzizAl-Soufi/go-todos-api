package todos

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/database/postgres/sqlc"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/shared/ids"
)

type postgresTodoRepo struct {
	queries *sqlc.Queries
}

func NewPostgresTodosRepository(db postgres.PostgresClient) TodosRepository {
	return &postgresTodoRepo{queries: sqlc.New(db.Pool())}
}

// Create inserts the todo; the database assigns ID and CreatedAt, which are
// written back into todo.
func (r *postgresTodoRepo) Create(ctx context.Context, userID string, todo *domain.Todo) error {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	created, err := r.queries.CreateTodo(ctx, sqlc.CreateTodoParams{UserID: userUUID, Title: todo.Title})
	if err != nil {
		return postgres.MapErr("create todo", err)
	}
	*todo = *postgresToDomainTodo(created)
	return nil
}

func (r *postgresTodoRepo) Update(ctx context.Context, id, userID string, dto *domain.UpdateTodoDTO) error {
	todoUUID, userUUID, err := ids.UUIDPair(id, userID)
	if err != nil {
		return err
	}
	if dto == nil || (dto.Title == nil && dto.Completed == nil) {
		return nil
	}

	current, err := r.queries.GetTodo(ctx, sqlc.GetTodoParams{ID: todoUUID, UserID: userUUID})
	if err != nil {
		return postgres.MapErr("get todo for update", err)
	}
	updated, _ := dto.UpdateEntity(postgresToDomainTodo(current))

	_, err = r.queries.UpdateTodo(ctx, sqlc.UpdateTodoParams{
		Title:     updated.Title,
		Completed: updated.Completed,
		ID:        todoUUID,
		UserID:    userUUID,
	})
	return postgres.MapErr("update todo", err)
}

func (r *postgresTodoRepo) Get(ctx context.Context, id, userID string) (*domain.Todo, error) {
	todoUUID, userUUID, err := ids.UUIDPair(id, userID)
	if err != nil {
		return nil, err
	}
	todo, err := r.queries.GetTodo(ctx, sqlc.GetTodoParams{ID: todoUUID, UserID: userUUID})
	if err != nil {
		return nil, postgres.MapErr("get todo", err)
	}
	return postgresToDomainTodo(todo), nil
}

func (r *postgresTodoRepo) Delete(ctx context.Context, id, userID string) error {
	todoUUID, userUUID, err := ids.UUIDPair(id, userID)
	if err != nil {
		return err
	}
	rows, err := r.queries.DeleteTodo(ctx, sqlc.DeleteTodoParams{ID: todoUUID, UserID: userUUID})
	return postgres.RequireAffected("delete todo", rows, err)
}

func (r *postgresTodoRepo) GetAll(ctx context.Context, userID string) ([]*domain.Todo, error) {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return nil, err
	}
	todos, err := r.queries.GetTodosByUser(ctx, userUUID)
	if err != nil {
		return nil, postgres.MapErr("get todos", err)
	}
	result := make([]*domain.Todo, 0, len(todos))
	for _, todo := range todos {
		result = append(result, postgresToDomainTodo(todo))
	}
	return result, nil
}

// DeleteByUserID removes all of a user's todos; having none is not an error.
func (r *postgresTodoRepo) DeleteByUserID(ctx context.Context, userID string) error {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	_, err = r.queries.DeleteTodosByUser(ctx, userUUID)
	return postgres.MapErr("delete todos by user", err)
}

func postgresToDomainTodo(todo sqlc.Todo) *domain.Todo {
	return &domain.Todo{
		ID:        todo.ID.String(),
		UserID:    todo.UserID.String(),
		Title:     todo.Title,
		Completed: todo.Completed,
		CreatedAt: todo.CreatedAt,
	}
}
