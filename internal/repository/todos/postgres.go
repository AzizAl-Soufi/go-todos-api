package todos

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/database/postgres/sqlc"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type postgresTodoRepo struct {
	queries *sqlc.Queries
}

func NewPostgresTodosRepository(db postgres.PostgresClient) TodosRepository {
	return &postgresTodoRepo{queries: sqlc.New(db.Pool())}
}

func (r *postgresTodoRepo) Create(ctx context.Context, userID string, todo *domain.Todo) error {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return err
	}
	if todo.ID == "" {
		todo.ID = uuid.NewString()
	}
	if todo.CreatedAt.IsZero() {
		todo.CreatedAt = time.Now().UTC()
	}
	created, err := r.queries.CreateTodo(ctx, sqlc.CreateTodoParams{UserID: parsedUserID, Title: todo.Title})
	if err != nil {
		return fmt.Errorf("create todo: %w", err)
	}
	*todo = *postgresToDomainTodo(created)
	return nil
}

func (r *postgresTodoRepo) Update(ctx context.Context, id, userID string, dto *domain.UpdateTodoDTO) error {
	parsedID, err := parseUUID(id)
	if err != nil {
		return err
	}
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return err
	}
	if dto == nil || (dto.Title == nil && dto.Completed == nil) {
		return nil
	}
	current, err := r.queries.GetTodo(ctx, sqlc.GetTodoParams{ID: parsedID, UserID: parsedUserID})
	if err != nil {
		return mapTodoError("get todo for update", err)
	}
	if dto.Title != nil {
		current.Title = *dto.Title
	}
	if dto.Completed != nil {
		current.Completed = *dto.Completed
	}
	_, err = r.queries.UpdateTodo(ctx, sqlc.UpdateTodoParams{Title: current.Title, Completed: current.Completed, ID: parsedID, UserID: parsedUserID})
	if err != nil {
		return mapTodoError("update todo", err)
	}
	return nil
}

func (r *postgresTodoRepo) Get(ctx context.Context, id, userID string) (*domain.Todo, error) {
	parsedID, err := parseUUID(id)
	if err != nil {
		return nil, err
	}
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return nil, err
	}
	todo, err := r.queries.GetTodo(ctx, sqlc.GetTodoParams{ID: parsedID, UserID: parsedUserID})
	if err != nil {
		return nil, mapTodoError("get todo", err)
	}
	return postgresToDomainTodo(todo), nil
}

func (r *postgresTodoRepo) Delete(ctx context.Context, id, userID string) error {
	parsedID, err := parseUUID(id)
	if err != nil {
		return err
	}
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return err
	}
	rows, err := r.queries.DeleteTodo(ctx, sqlc.DeleteTodoParams{ID: parsedID, UserID: parsedUserID})
	if err != nil {
		return fmt.Errorf("delete todo: %w", err)
	}
	if rows == 0 {
		return apperrors.ErrNotFound
	}

	return nil
}

func (r *postgresTodoRepo) GetAll(ctx context.Context, userID string) ([]*domain.Todo, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return nil, err
	}
	todos, err := r.queries.GetTodosByUser(ctx, parsedUserID)
	if err != nil {
		return nil, fmt.Errorf("get todos: %w", err)
	}
	domainTodos := make([]*domain.Todo, 0, len(todos))
	for _, todo := range todos {
		domainTodos = append(domainTodos, postgresToDomainTodo(todo))
	}
	return domainTodos, nil
}

func (r *postgresTodoRepo) DeleteByUserID(ctx context.Context, userID string) error {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return err
	}
	_, err = r.queries.DeleteTodosByUser(ctx, parsedUserID)
	if err != nil {
		return fmt.Errorf("delete todos by user: %w", err)
	}
	return nil
}

func parseUUID(value string) (uuid.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, apperrors.Validation("INVALID_ID", "invalid id format")
	}
	return parsed, nil
}

func mapTodoError(context string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperrors.ErrNotFound
	}
	return fmt.Errorf("%s: %w", context, err)
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
