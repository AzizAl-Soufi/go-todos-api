// Package pgstore holds the PostgreSQL audit event store. Each call runs on
// the transaction carried by ctx when there is one, otherwise on the pool.
package pgstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/pgstore/sqlcgen"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
)

type Events struct{ pool *pgxpool.Pool }

var _ application.EventStore = (*Events)(nil)

func NewEvents(pool *pgxpool.Pool) *Events { return &Events{pool} }

func (s *Events) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Executor(ctx, s.pool))
}

func optionalUUID(id string) (*uuid.UUID, error) {
	if id == "" {
		return nil, nil
	}
	u, err := ids.UUID(id)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func idString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func (s *Events) Append(ctx context.Context, e *domain.Event) error {
	var teamID, userID, actorID *uuid.UUID
	for _, p := range []struct {
		dst **uuid.UUID
		id  string
	}{{&teamID, e.TeamID}, {&userID, e.UserID}, {&actorID, e.ActorID}} {
		u, err := optionalUUID(p.id)
		if err != nil {
			return err
		}
		*p.dst = u
	}
	details := e.Details
	if details == nil {
		details = map[string]string{}
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode audit details: %w", err)
	}
	id, err := s.q(ctx).AppendEvent(ctx, sqlcgen.AppendEventParams{
		TeamID: teamID, UserID: userID, ActorID: actorID, Action: e.Action, TargetType: e.TargetType,
		TargetID: e.TargetID, Details: raw, Ip: e.IP, UserAgent: e.UserAgent, CreatedAt: e.CreatedAt,
	})
	if err != nil {
		return postgres.MapErr("append audit event", err)
	}
	e.ID = id.String()
	return nil
}

func (s *Events) List(ctx context.Context, f domain.Filter) ([]*domain.Event, error) {
	teamID, err := optionalUUID(f.TeamID)
	if err != nil {
		return nil, err
	}
	userID, err := optionalUUID(f.UserID)
	if err != nil {
		return nil, err
	}
	params := sqlcgen.ListEventsParams{TeamID: teamID, UserID: userID, MaxRows: int32(f.Limit)}
	if f.Before != nil {
		beforeID, err := ids.UUID(f.Before.ID)
		if err != nil {
			return nil, err
		}
		params.BeforeAt, params.BeforeID = &f.Before.At, &beforeID
	}
	rows, err := s.q(ctx).ListEvents(ctx, params)
	if err != nil {
		return nil, postgres.MapErr("list audit events", err)
	}
	out := make([]*domain.Event, 0, len(rows))
	for _, r := range rows {
		details := map[string]string{}
		if err := json.Unmarshal(r.Details, &details); err != nil {
			return nil, fmt.Errorf("decode audit details: %w", err)
		}
		out = append(out, &domain.Event{
			ID: r.ID.String(), TeamID: idString(r.TeamID), UserID: idString(r.UserID), ActorID: idString(r.ActorID),
			Action: r.Action, TargetType: r.TargetType, TargetID: r.TargetID, Details: details,
			IP: r.Ip, UserAgent: r.UserAgent, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

func (s *Events) DeleteByUser(ctx context.Context, userID string) error {
	u, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete audit events by user", s.q(ctx).DeleteEventsByUser(ctx, &u))
}

func (s *Events) DeleteByTeam(ctx context.Context, teamID string) error {
	u, err := ids.UUID(teamID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete audit events by team", s.q(ctx).DeleteEventsByTeam(ctx, &u))
}
