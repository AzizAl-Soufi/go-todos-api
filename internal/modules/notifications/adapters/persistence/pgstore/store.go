// Package pgstore holds the PostgreSQL device store. Each call runs on the
// transaction carried by ctx when there is one, otherwise on the pool.
package pgstore

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/adapters/persistence/pgstore/sqlcgen"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
)

type Devices struct{ pool *pgxpool.Pool }

var _ application.DeviceRepository = (*Devices)(nil)

func NewDevices(pool *pgxpool.Pool) *Devices { return &Devices{pool} }

func (s *Devices) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Executor(ctx, s.pool))
}

func toDomain(rows []sqlcgen.Device, err error) ([]*domain.Device, error) {
	if err != nil {
		return nil, postgres.MapErr("list devices", err)
	}
	out := make([]*domain.Device, 0, len(rows))
	for _, r := range rows {
		out = append(out, &domain.Device{
			Token:     r.Token,
			UserID:    r.UserID.String(),
			Platform:  domain.Platform(r.Platform),
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		})
	}
	return out, nil
}

func (s *Devices) Save(ctx context.Context, d *domain.Device) error {
	uid, err := ids.UUID(d.UserID)
	if err != nil {
		return err
	}
	err = s.q(ctx).SaveDevice(ctx, sqlcgen.SaveDeviceParams{
		Token:     d.Token,
		UserID:    uid,
		Platform:  string(d.Platform),
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	})
	return postgres.MapErr("save device", err)
}

func (s *Devices) ListForUser(ctx context.Context, userID string) ([]*domain.Device, error) {
	uid, err := ids.UUID(userID)
	if err != nil {
		return nil, err
	}
	return toDomain(s.q(ctx).ListUserDevices(ctx, uid))
}

func (s *Devices) ListForUsers(ctx context.Context, userIDs []string) ([]*domain.Device, error) {
	uids := make([]uuid.UUID, 0, len(userIDs))
	for _, id := range userIDs {
		u, err := ids.UUID(id)
		if err != nil {
			return nil, err
		}
		uids = append(uids, u)
	}
	return toDomain(s.q(ctx).ListDevicesOfUsers(ctx, uids))
}

func (s *Devices) Delete(ctx context.Context, userID, token string) error {
	uid, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteDevice(ctx, sqlcgen.DeleteDeviceParams{Token: token, UserID: uid})
	return postgres.RequireAffected("delete device", rows, err)
}

func (s *Devices) DeleteTokens(ctx context.Context, tokens []string) error {
	return postgres.MapErr("delete device tokens", s.q(ctx).DeleteDeviceTokens(ctx, tokens))
}

func (s *Devices) DeleteForUser(ctx context.Context, userID string) error {
	uid, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete user devices", s.q(ctx).DeleteUserDevices(ctx, uid))
}
