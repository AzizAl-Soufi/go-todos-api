// Package pgstore holds the PostgreSQL identity repositories. Each call runs
// on the transaction carried by ctx when there is one, otherwise on the pool.
package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/pgstore/sqlcgen"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pgUniqueViolation = "23505"

type base struct{ pool *pgxpool.Pool }

func (b base) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Executor(ctx, b.pool))
}

// ---- users ----

type Users struct{ base }

var _ application.UserRepository = (*Users)(nil)

func NewUsers(pool *pgxpool.Pool) *Users { return &Users{base{pool}} }

func userToDomain(u sqlcgen.User) *domain.User {
	return &domain.User{
		ID: u.ID.String(), Name: u.Name, Email: u.Email, PasswordHash: u.PasswordHash,
		EmailVerifiedAt: u.EmailVerifiedAt, TokenVersion: int(u.TokenVersion), CreatedAt: u.CreatedAt,
	}
}

func (s *Users) Create(ctx context.Context, u *domain.User) error {
	row, err := s.q(ctx).CreateUser(ctx, sqlcgen.CreateUserParams{
		Name: u.Name, Email: u.Email, PasswordHash: u.PasswordHash, CreatedAt: u.CreatedAt,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return domain.ErrEmailTaken
		}
		return postgres.MapErr("create user", err)
	}
	*u = *userToDomain(row)
	return nil
}

func (s *Users) FindByID(ctx context.Context, id string) (*domain.User, error) {
	uid, err := ids.UUID(id)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetUserByID(ctx, uid)
	if err != nil {
		return nil, postgres.MapErr("get user", err)
	}
	return userToDomain(row), nil
}

func (s *Users) FindByEmail(ctx context.Context, email string) (*domain.User, error) {
	row, err := s.q(ctx).GetUserByEmail(ctx, email)
	if err != nil {
		return nil, postgres.MapErr("get user by email", err)
	}
	return userToDomain(row), nil
}

func (s *Users) MarkEmailVerified(ctx context.Context, id string, at time.Time) error {
	uid, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).MarkEmailVerified(ctx, sqlcgen.MarkEmailVerifiedParams{ID: uid, EmailVerifiedAt: &at})
	return postgres.RequireAffected("mark email verified", rows, err)
}

func (s *Users) IncrementTokenVersion(ctx context.Context, id string) error {
	uid, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).IncrementTokenVersion(ctx, uid)
	return postgres.RequireAffected("increment token version", rows, err)
}

func (s *Users) SetPasswordHash(ctx context.Context, id, hash string) error {
	uid, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).SetPasswordHash(ctx, sqlcgen.SetPasswordHashParams{ID: uid, PasswordHash: hash})
	return postgres.RequireAffected("set password hash", rows, err)
}

func (s *Users) Delete(ctx context.Context, id string) error {
	uid, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteUser(ctx, uid)
	return postgres.RequireAffected("delete user", rows, err)
}

// ---- sessions ----

type Sessions struct{ base }

var _ application.SessionRepository = (*Sessions)(nil)

func NewSessions(pool *pgxpool.Pool) *Sessions { return &Sessions{base{pool}} }

func (s *Sessions) Create(ctx context.Context, sess *domain.Session) error {
	userID, familyID, err := ids.UUIDPair(sess.UserID, sess.FamilyID)
	if err != nil {
		return err
	}
	row, err := s.q(ctx).CreateSession(ctx, sqlcgen.CreateSessionParams{
		UserID: userID, FamilyID: familyID, TokenHash: sess.TokenHash, ExpiresAt: sess.ExpiresAt, CreatedAt: sess.CreatedAt,
	})
	if err != nil {
		return postgres.MapErr("create session", err)
	}
	sess.ID = row.ID.String()
	return nil
}

func (s *Sessions) FindByTokenHash(ctx context.Context, tokenHash string) (*domain.Session, error) {
	row, err := s.q(ctx).GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, postgres.MapErr("get session", err)
	}
	return &domain.Session{
		ID: row.ID.String(), UserID: row.UserID.String(), FamilyID: row.FamilyID.String(), TokenHash: row.TokenHash,
		ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt, CreatedAt: row.CreatedAt,
	}, nil
}

func (s *Sessions) Revoke(ctx context.Context, id string, at time.Time) (bool, error) {
	sid, err := ids.UUID(id)
	if err != nil {
		return false, err
	}
	rows, err := s.q(ctx).RevokeSession(ctx, sqlcgen.RevokeSessionParams{ID: sid, RevokedAt: &at})
	if err != nil {
		return false, postgres.MapErr("revoke session", err)
	}
	return rows == 1, nil
}

func (s *Sessions) RevokeFamily(ctx context.Context, familyID string, at time.Time) error {
	fid, err := ids.UUID(familyID)
	if err != nil {
		return err
	}
	err = s.q(ctx).RevokeSessionFamily(ctx, sqlcgen.RevokeSessionFamilyParams{FamilyID: fid, RevokedAt: &at})
	return postgres.MapErr("revoke session family", err)
}

func (s *Sessions) RevokeAllForUser(ctx context.Context, userID string, at time.Time) error {
	uid, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	err = s.q(ctx).RevokeUserSessions(ctx, sqlcgen.RevokeUserSessionsParams{UserID: uid, RevokedAt: &at})
	return postgres.MapErr("revoke user sessions", err)
}

func (s *Sessions) DeleteAllForUser(ctx context.Context, userID string) error {
	uid, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete user sessions", s.q(ctx).DeleteUserSessions(ctx, uid))
}

// ---- one-time tokens ----

// OneTimeTokens stores one kind of one-time token. email_verifications and
// password_resets have identical columns, so their generated sqlc types
// convert into each other and one implementation serves both tables.
type OneTimeTokens struct {
	base
	kind    string // for error messages
	upsert  func(*sqlcgen.Queries, context.Context, sqlcgen.UpsertEmailVerificationParams) error
	consume func(*sqlcgen.Queries, context.Context, string) (sqlcgen.EmailVerification, error)
	remove  func(*sqlcgen.Queries, context.Context, uuid.UUID) error
}

var _ application.OneTimeTokenRepository = (*OneTimeTokens)(nil)

func NewVerifications(pool *pgxpool.Pool) *OneTimeTokens {
	return &OneTimeTokens{
		base:    base{pool},
		kind:    "email verification",
		upsert:  (*sqlcgen.Queries).UpsertEmailVerification,
		consume: (*sqlcgen.Queries).ConsumeEmailVerification,
		remove:  (*sqlcgen.Queries).DeleteEmailVerification,
	}
}

func NewPasswordResets(pool *pgxpool.Pool) *OneTimeTokens {
	return &OneTimeTokens{
		base: base{pool},
		kind: "password reset",
		upsert: func(q *sqlcgen.Queries, ctx context.Context, p sqlcgen.UpsertEmailVerificationParams) error {
			return q.UpsertPasswordReset(ctx, sqlcgen.UpsertPasswordResetParams(p))
		},
		consume: func(q *sqlcgen.Queries, ctx context.Context, tokenHash string) (sqlcgen.EmailVerification, error) {
			row, err := q.ConsumePasswordReset(ctx, tokenHash)
			return sqlcgen.EmailVerification(row), err
		},
		remove: (*sqlcgen.Queries).DeletePasswordReset,
	}
}

func (s *OneTimeTokens) Save(ctx context.Context, t *domain.OneTimeToken) error {
	uid, err := ids.UUID(t.UserID)
	if err != nil {
		return err
	}
	err = s.upsert(s.q(ctx), ctx, sqlcgen.UpsertEmailVerificationParams{
		UserID: uid, TokenHash: t.TokenHash, ExpiresAt: t.ExpiresAt, CreatedAt: t.CreatedAt,
	})
	return postgres.MapErr("save "+s.kind, err)
}

func (s *OneTimeTokens) Consume(ctx context.Context, tokenHash string) (*domain.OneTimeToken, error) {
	row, err := s.consume(s.q(ctx), ctx, tokenHash)
	if err != nil {
		return nil, postgres.MapErr("consume "+s.kind, err)
	}
	return &domain.OneTimeToken{UserID: row.UserID.String(), TokenHash: row.TokenHash, ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt}, nil
}

func (s *OneTimeTokens) DeleteForUser(ctx context.Context, userID string) error {
	uid, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete "+s.kind, s.remove(s.q(ctx), ctx, uid))
}
