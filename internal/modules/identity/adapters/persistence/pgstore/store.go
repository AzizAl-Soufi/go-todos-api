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
	d := &domain.User{
		ID: u.ID.String(), Name: u.Name, Email: u.Email, EmailVerifiedAt: u.EmailVerifiedAt,
		SessionsValidAfter: u.SessionsValidAfter, CreatedAt: u.CreatedAt,
	}
	if u.FirebaseUid != nil {
		d.FirebaseUID = *u.FirebaseUid
	}
	return d
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// uniqueViolation maps a unique violation to the port error of its index.
func uniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgUniqueViolation {
		return nil
	}
	if pgErr.ConstraintName == "idx_users_firebase_uid" {
		return domain.ErrFirebaseUIDTaken
	}
	return domain.ErrEmailTaken
}

func (s *Users) Create(ctx context.Context, u *domain.User) error {
	row, err := s.q(ctx).CreateUser(ctx, sqlcgen.CreateUserParams{
		Name: u.Name, Email: u.Email, FirebaseUid: nullable(u.FirebaseUID),
		EmailVerifiedAt: u.EmailVerifiedAt, CreatedAt: u.CreatedAt,
	})
	if err != nil {
		if mapped := uniqueViolation(err); mapped != nil {
			return mapped
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

func (s *Users) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*domain.User, error) {
	row, err := s.q(ctx).GetUserByFirebaseUID(ctx, &firebaseUID)
	if err != nil {
		return nil, postgres.MapErr("get user by firebase uid", err)
	}
	return userToDomain(row), nil
}

func (s *Users) LinkFirebaseUID(ctx context.Context, id, firebaseUID string) error {
	uid, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).LinkFirebaseUID(ctx, sqlcgen.LinkFirebaseUIDParams{ID: uid, FirebaseUid: &firebaseUID})
	if err != nil {
		if mapped := uniqueViolation(err); mapped != nil {
			return mapped
		}
		return postgres.MapErr("link firebase uid", err)
	}
	if rows == 0 {
		if _, err := s.FindByID(ctx, id); err != nil {
			return err
		}
		return domain.ErrFirebaseUIDTaken // the account is linked already
	}
	return nil
}

func (s *Users) MarkEmailVerified(ctx context.Context, id string, at time.Time) error {
	uid, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).MarkEmailVerified(ctx, sqlcgen.MarkEmailVerifiedParams{ID: uid, EmailVerifiedAt: &at})
	return postgres.RequireAffected("mark email verified", rows, err)
}

func (s *Users) RevokeSessions(ctx context.Context, id string, at time.Time) error {
	uid, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).RevokeUserSessions(ctx, sqlcgen.RevokeUserSessionsParams{ID: uid, SessionsValidAfter: &at})
	return postgres.RequireAffected("revoke sessions", rows, err)
}

func (s *Users) Delete(ctx context.Context, id string) error {
	uid, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteUser(ctx, uid)
	return postgres.RequireAffected("delete user", rows, err)
}

// ---- passkeys ----

type Passkeys struct{ base }

var _ application.PasskeyRepository = (*Passkeys)(nil)

func NewPasskeys(pool *pgxpool.Pool) *Passkeys { return &Passkeys{base{pool}} }

func (s *Passkeys) Create(ctx context.Context, p *domain.Passkey) error {
	uid, err := ids.UUID(p.UserID)
	if err != nil {
		return err
	}
	id, err := s.q(ctx).CreatePasskey(ctx, sqlcgen.CreatePasskeyParams{
		UserID: uid, Name: p.Name, CredentialID: p.CredentialID, Credential: p.Credential, CreatedAt: p.CreatedAt,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return domain.ErrPasskeyExists
		}
		return postgres.MapErr("create passkey", err)
	}
	p.ID = id.String()
	return nil
}

func (s *Passkeys) ListForUser(ctx context.Context, userID string) ([]*domain.Passkey, error) {
	uid, err := ids.UUID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).ListUserPasskeys(ctx, uid)
	if err != nil {
		return nil, postgres.MapErr("list passkeys", err)
	}
	out := make([]*domain.Passkey, 0, len(rows))
	for _, r := range rows {
		out = append(out, &domain.Passkey{
			ID: r.ID.String(), UserID: r.UserID.String(), Name: r.Name, CredentialID: r.CredentialID,
			Credential: r.Credential, CreatedAt: r.CreatedAt, LastUsedAt: r.LastUsedAt,
		})
	}
	return out, nil
}

func (s *Passkeys) RecordUse(ctx context.Context, id string, credential []byte, at time.Time) error {
	pid, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).RecordPasskeyUse(ctx, sqlcgen.RecordPasskeyUseParams{ID: pid, Credential: credential, LastUsedAt: &at})
	return postgres.RequireAffected("record passkey use", rows, err)
}

func (s *Passkeys) Delete(ctx context.Context, userID, id string) error {
	uid, pid, err := ids.UUIDPair(userID, id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeletePasskey(ctx, sqlcgen.DeletePasskeyParams{ID: pid, UserID: uid})
	return postgres.RequireAffected("delete passkey", rows, err)
}

func (s *Passkeys) DeleteAllForUser(ctx context.Context, userID string) error {
	uid, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete user passkeys", s.q(ctx).DeleteUserPasskeys(ctx, uid))
}

// ---- passkey challenges ----

type PasskeyChallenges struct{ base }

var _ application.PasskeyChallengeRepository = (*PasskeyChallenges)(nil)

func NewPasskeyChallenges(pool *pgxpool.Pool) *PasskeyChallenges {
	return &PasskeyChallenges{base{pool}}
}

func (s *PasskeyChallenges) Save(ctx context.Context, c *domain.PasskeyChallenge) error {
	var uid *uuid.UUID
	if c.UserID != "" {
		id, err := ids.UUID(c.UserID)
		if err != nil {
			return err
		}
		uid = &id
	}
	q := s.q(ctx)
	if err := q.DeleteExpiredPasskeyChallenges(ctx, c.CreatedAt); err != nil {
		return postgres.MapErr("prune passkey challenges", err)
	}
	err := q.CreatePasskeyChallenge(ctx, sqlcgen.CreatePasskeyChallengeParams{
		TokenHash: c.TokenHash, UserID: uid, State: c.State, ExpiresAt: c.ExpiresAt, CreatedAt: c.CreatedAt,
	})
	return postgres.MapErr("save passkey challenge", err)
}

func (s *PasskeyChallenges) Consume(ctx context.Context, tokenHash string) (*domain.PasskeyChallenge, error) {
	row, err := s.q(ctx).ConsumePasskeyChallenge(ctx, tokenHash)
	if err != nil {
		return nil, postgres.MapErr("consume passkey challenge", err)
	}
	c := &domain.PasskeyChallenge{TokenHash: row.TokenHash, State: row.State, ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt}
	if row.UserID != nil {
		c.UserID = row.UserID.String()
	}
	return c, nil
}

func (s *PasskeyChallenges) DeleteForUser(ctx context.Context, userID string) error {
	uid, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete user passkey challenges", s.q(ctx).DeleteUserPasskeyChallenges(ctx, &uid))
}
