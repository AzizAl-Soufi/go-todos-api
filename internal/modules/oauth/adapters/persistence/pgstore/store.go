// Package pgstore holds the oauth module's PostgreSQL stores. Each call runs
// on the transaction carried by ctx when there is one, otherwise on the pool.
package pgstore

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/pgstore/sqlcgen"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
)

type base struct{ pool *pgxpool.Pool }

func (b base) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Executor(ctx, b.pool))
}

// ---- clients ----

type Clients struct{ base }

var _ application.ClientRepository = (*Clients)(nil)

func NewClients(pool *pgxpool.Pool) *Clients { return &Clients{base{pool}} }

func (s *Clients) Create(ctx context.Context, c *domain.Client) error {
	id, err := s.q(ctx).CreateClient(ctx, sqlcgen.CreateClientParams{
		Name:         c.Name,
		RedirectUris: c.RedirectURIs,
		CreatedAt:    c.CreatedAt,
	})
	if err != nil {
		return postgres.MapErr("create oauth client", err)
	}
	c.ID = id.String()
	return nil
}

func (s *Clients) Get(ctx context.Context, id string) (*domain.Client, error) {
	u, err := ids.UUID(id)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetClient(ctx, u)
	if err != nil {
		return nil, postgres.MapErr("get oauth client", err)
	}
	return &domain.Client{
		ID:           row.ID.String(),
		Name:         row.Name,
		RedirectURIs: slices.Clone(row.RedirectUris),
		Kind:         domain.Registered,
		CreatedAt:    row.CreatedAt.UTC(),
	}, nil
}

func (s *Clients) DeleteUnused(ctx context.Context, createdBefore time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	n, err := s.q(ctx).DeleteUnusedClients(ctx, sqlcgen.DeleteUnusedClientsParams{
		CreatedBefore: createdBefore, MaxRows: maxRows(limit),
	})
	return n, postgres.MapErr("delete unused oauth clients", err)
}

func maxRows(limit int) int32 { return int32(min(limit, math.MaxInt32)) }

// ---- codes ----

type Codes struct{ base }

var _ application.CodeRepository = (*Codes)(nil)

func NewCodes(pool *pgxpool.Pool) *Codes { return &Codes{base{pool}} }

func (s *Codes) Save(ctx context.Context, c *domain.Code) error {
	userID, err := ids.UUID(c.Owner.UserID)
	if err != nil {
		return err
	}
	q := s.q(ctx)
	if err := q.DeleteExpiredCodes(ctx, c.CreatedAt); err != nil {
		return postgres.MapErr("delete expired oauth codes", err)
	}
	err = q.CreateCode(ctx, sqlcgen.CreateCodeParams{
		CodeHash:      c.Hash,
		ClientID:      c.ClientID,
		ClientName:    c.ClientName,
		RedirectUri:   c.RedirectURI,
		Challenge:     c.Challenge,
		Resource:      c.Resource,
		Scope:         c.Scope,
		UserID:        userID,
		UserName:      c.Owner.Name,
		UserEmail:     c.Owner.Email,
		EmailVerified: c.Owner.EmailVerified,
		ExpiresAt:     c.ExpiresAt,
		CreatedAt:     c.CreatedAt,
	})
	return postgres.MapErr("create oauth code", err)
}

func (s *Codes) Consume(ctx context.Context, hash string) (*domain.Code, error) {
	row, err := s.q(ctx).ConsumeCode(ctx, hash)
	if err != nil {
		return nil, postgres.MapErr("consume oauth code", err)
	}
	return &domain.Code{
		Hash:        row.CodeHash,
		ClientID:    row.ClientID,
		ClientName:  row.ClientName,
		RedirectURI: row.RedirectUri,
		Challenge:   row.Challenge,
		Resource:    row.Resource,
		Scope:       row.Scope,
		Owner: domain.Owner{
			UserID:        row.UserID.String(),
			Name:          row.UserName,
			Email:         row.UserEmail,
			EmailVerified: row.EmailVerified,
		},
		ExpiresAt: row.ExpiresAt.UTC(),
		CreatedAt: row.CreatedAt.UTC(),
	}, nil
}

func (s *Codes) DeleteForUser(ctx context.Context, userID string) error {
	u, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete user oauth codes", s.q(ctx).DeleteUserCodes(ctx, u))
}

func (s *Codes) DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	n, err := s.q(ctx).DeleteExpiredCodesBatch(ctx, sqlcgen.DeleteExpiredCodesBatchParams{Now: now, MaxRows: maxRows(limit)})
	return n, postgres.MapErr("delete expired oauth codes", err)
}

// ---- grants ----

type Grants struct{ base }

var _ application.GrantRepository = (*Grants)(nil)

func NewGrants(pool *pgxpool.Pool) *Grants { return &Grants{base{pool}} }

func grantToDomain(g sqlcgen.OauthGrant) *domain.Grant {
	return &domain.Grant{
		ID:         g.ID.String(),
		ClientID:   g.ClientID,
		ClientName: g.ClientName,
		Scope:      g.Scope,
		Resource:   g.Resource,
		Owner: domain.Owner{
			UserID:        g.UserID.String(),
			Name:          g.UserName,
			Email:         g.UserEmail,
			EmailVerified: g.EmailVerified,
		},
		AccessHash:       g.AccessHash,
		AccessExpiresAt:  g.AccessExpiresAt.UTC(),
		RefreshHash:      g.RefreshHash,
		RefreshExpiresAt: g.RefreshExpiresAt.UTC(),
		CreatedAt:        g.CreatedAt.UTC(),
		LastUsedAt:       g.LastUsedAt.UTC(),
	}
}

func (s *Grants) Create(ctx context.Context, g *domain.Grant) error {
	userID, err := ids.UUID(g.Owner.UserID)
	if err != nil {
		return err
	}
	q := s.q(ctx)
	err = q.DeleteExpiredUserGrants(ctx, sqlcgen.DeleteExpiredUserGrantsParams{UserID: userID, RefreshExpiresAt: g.CreatedAt})
	if err != nil {
		return postgres.MapErr("delete expired oauth grants", err)
	}
	id, err := q.CreateGrant(ctx, sqlcgen.CreateGrantParams{
		ClientID:         g.ClientID,
		ClientName:       g.ClientName,
		Scope:            g.Scope,
		Resource:         g.Resource,
		UserID:           userID,
		UserName:         g.Owner.Name,
		UserEmail:        g.Owner.Email,
		EmailVerified:    g.Owner.EmailVerified,
		AccessHash:       g.AccessHash,
		AccessExpiresAt:  g.AccessExpiresAt,
		RefreshHash:      g.RefreshHash,
		RefreshExpiresAt: g.RefreshExpiresAt,
		CreatedAt:        g.CreatedAt,
		LastUsedAt:       g.LastUsedAt,
	})
	if err != nil {
		return postgres.MapErr("create oauth grant", err)
	}
	g.ID = id.String()
	return nil
}

func (s *Grants) Get(ctx context.Context, id string) (*domain.Grant, error) {
	u, err := ids.UUID(id)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetGrant(ctx, u)
	if err != nil {
		return nil, postgres.MapErr("get oauth grant", err)
	}
	return grantToDomain(row), nil
}

func (s *Grants) ListForUser(ctx context.Context, userID string) ([]*domain.Grant, error) {
	u, err := ids.UUID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).ListUserGrants(ctx, u)
	if err != nil {
		return nil, postgres.MapErr("list oauth grants", err)
	}
	out := make([]*domain.Grant, 0, len(rows))
	for _, r := range rows {
		out = append(out, grantToDomain(r))
	}
	return out, nil
}

func (s *Grants) Rotate(ctx context.Context, r application.Rotation) error {
	u, err := ids.UUID(r.GrantID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).RotateGrant(ctx, sqlcgen.RotateGrantParams{
		ID:               u,
		RefreshHash:      r.RefreshHash,
		AccessHash:       r.AccessHash,
		AccessExpiresAt:  r.AccessExpiresAt,
		NewRefreshHash:   r.NewRefreshHash,
		RefreshExpiresAt: r.RefreshExpiresAt,
		UsedAt:           r.UsedAt,
	})
	return postgres.RequireAffected("rotate oauth grant", rows, err)
}

func (s *Grants) Delete(ctx context.Context, userID, id string) error {
	grantID, owner, err := ids.UUIDPair(id, userID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteGrant(ctx, sqlcgen.DeleteGrantParams{ID: grantID, UserID: owner})
	return postgres.RequireAffected("delete oauth grant", rows, err)
}

func (s *Grants) Revoke(ctx context.Context, id string) error {
	u, err := ids.UUID(id)
	if errors.Is(err, apperrors.ErrInvalidID) {
		return nil // can't name a stored grant
	}
	if err != nil {
		return err
	}
	return postgres.MapErr("revoke oauth grant", s.q(ctx).RevokeGrant(ctx, u))
}

func (s *Grants) DeleteForUser(ctx context.Context, userID string) error {
	u, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete user oauth grants", s.q(ctx).DeleteUserGrants(ctx, u))
}

func (s *Grants) DeleteExpired(ctx context.Context, now time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	n, err := s.q(ctx).DeleteExpiredGrants(ctx, sqlcgen.DeleteExpiredGrantsParams{Now: now, MaxRows: maxRows(limit)})
	return n, postgres.MapErr("delete expired oauth grants", err)
}
