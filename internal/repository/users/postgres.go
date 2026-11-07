package users

import (
	"context"
	"errors"

	"github.com/AzizAl-Soufi/go-todos-api/internal/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/database/postgres/sqlc"
	"github.com/AzizAl-Soufi/go-todos-api/internal/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/shared/ids"
	"github.com/jackc/pgx/v5/pgconn"
)

const pgUniqueViolation = "23505"

type postgresUserRepo struct {
	queries *sqlc.Queries
}

func NewPostgresUsersRepository(db postgres.PostgresClient) UsersRepository {
	return &postgresUserRepo{queries: sqlc.New(db.Pool())}
}

// Register creates the user, or returns the existing one if the email is
// already taken (the unique constraint makes this race-free).
func (r *postgresUserRepo) Register(ctx context.Context, user *domain.UserDTO) (*domain.User, error) {
	created, err := r.queries.CreateUser(ctx, sqlc.CreateUserParams{Name: user.Name, Email: user.Email})
	if err == nil {
		return toDomainUser(created, true), nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgUniqueViolation {
		return nil, postgres.MapErr("create user", err)
	}

	existing, err := r.queries.GetUserByEmail(ctx, user.Email)
	if err != nil {
		return nil, postgres.MapErr("get existing user", err)
	}
	return toDomainUser(existing, false), nil
}

func (r *postgresUserRepo) Auth(ctx context.Context, id string) (*domain.User, error) {
	return r.getByID(ctx, id)
}

func (r *postgresUserRepo) GetOverview(ctx context.Context, id string) (*domain.User, error) {
	return r.getByID(ctx, id)
}

func (r *postgresUserRepo) Delete(ctx context.Context, id string) error {
	userUUID, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := r.queries.DeleteUser(ctx, userUUID)
	return postgres.RequireAffected("delete user", rows, err)
}

func (r *postgresUserRepo) getByID(ctx context.Context, id string) (*domain.User, error) {
	userUUID, err := ids.UUID(id)
	if err != nil {
		return nil, err
	}
	user, err := r.queries.GetUserByID(ctx, userUUID)
	if err != nil {
		return nil, postgres.MapErr("get user", err)
	}
	return toDomainUser(user, false), nil
}

func toDomainUser(u sqlc.User, isNew bool) *domain.User {
	return &domain.User{
		ID:        u.ID.String(),
		Name:      u.Name,
		Email:     u.Email,
		CreatedAt: u.CreatedAt,
		IsNew:     isNew,
	}
}
