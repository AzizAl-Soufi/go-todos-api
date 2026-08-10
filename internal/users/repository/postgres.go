package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/common/errors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/pkg/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/pkg/database/postgres/sqlc"
	"github.com/AzizAl-Soufi/go-todos-api/internal/users/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresUserRepo struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

func NewPostgresUsersRepository(db postgres.PostgresClient) UsersRepository {
	pool := db.Pool()
	return &postgresUserRepo{pool: pool, queries: sqlc.New(pool)}
}

func (r *postgresUserRepo) Register(ctx context.Context, user *domain.UserDTO) (*domain.User, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin user registration: %w", err)
	}
	defer tx.Rollback(ctx)

	createdAt := time.Now().UTC()
	created, err := r.queries.WithTx(tx).CreateUser(ctx, sqlc.CreateUserParams{Name: user.Name, Email: user.Email})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return nil, fmt.Errorf("rollback existing user registration: %w", rollbackErr)
			}
			existing, lookupErr := r.queries.GetUserByEmail(ctx, user.Email)
			if lookupErr != nil {
				if errors.Is(lookupErr, pgx.ErrNoRows) {
					return nil, apperrors.ErrNotFound
				}
				return nil, fmt.Errorf("get existing user: %w", lookupErr)
			}

			return &domain.User{
				ID:        existing.ID.String(),
				Name:      existing.Name,
				Email:     existing.Email,
				CreatedAt: existing.CreatedAt,
				IsNew:     false,
			}, nil
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit user registration: %w", err)
	}
	if created.CreatedAt.IsZero() {
		created.CreatedAt = createdAt
	}
	return &domain.User{ID: created.ID.String(), Name: created.Name, Email: created.Email, CreatedAt: created.CreatedAt, IsNew: true}, nil
}

func (r *postgresUserRepo) Auth(ctx context.Context, id string) (*domain.User, error) {
	return r.getByID(ctx, id)
}

func (r *postgresUserRepo) GetOverview(ctx context.Context, id string) (*domain.User, error) {
	return r.getByID(ctx, id)
}

func (r *postgresUserRepo) Delete(ctx context.Context, id string) error {
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return apperrors.Validation("INVALID_ID", "invalid id format")
	}
	rows, err := r.queries.DeleteUser(ctx, parsedID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if rows == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *postgresUserRepo) getByID(ctx context.Context, id string) (*domain.User, error) {
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return nil, apperrors.Validation("INVALID_ID", "invalid id format")
	}
	user, err := r.queries.GetUserByID(ctx, parsedID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperrors.ErrNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return &domain.User{ID: user.ID.String(), Name: user.Name, Email: user.Email, CreatedAt: user.CreatedAt}, nil
}
