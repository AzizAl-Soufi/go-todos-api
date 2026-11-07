// Package command holds the audit module's write use cases.
package command

import (
	"context"
	"maps"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/domain"
)

type Deps struct {
	Events application.EventRepository
	Now    func() time.Time // nil means time.Now
}

type Handlers struct {
	Record        RecordHandler
	DeleteUserLog DeleteUserLogHandler
	DeleteTeamLog DeleteTeamLogHandler
}

func New(d Deps) Handlers {
	if d.Now == nil {
		d.Now = time.Now
	}
	return Handlers{Record: RecordHandler{d}, DeleteUserLog: DeleteUserLogHandler{d}, DeleteTeamLog: DeleteTeamLogHandler{d}}
}

// Record appends an event, stamped with the current time.
type Record struct {
	TeamID, UserID, ActorID string
	Action                  string
	TargetType, TargetID    string
	Details                 map[string]string
	IP, UserAgent           string
}

type RecordHandler struct{ Deps }

func (h RecordHandler) Handle(ctx context.Context, cmd Record) error {
	details := maps.Clone(cmd.Details)
	if details == nil {
		details = map[string]string{}
	}
	e := &domain.Event{
		TeamID: cmd.TeamID, UserID: cmd.UserID, ActorID: cmd.ActorID, Action: cmd.Action,
		TargetType: cmd.TargetType, TargetID: cmd.TargetID, Details: details,
		IP: cmd.IP, UserAgent: cmd.UserAgent, CreatedAt: domain.Timestamp(h.Now()),
	}
	if err := e.Validate(); err != nil {
		return err
	}
	return h.Events.Append(ctx, e)
}

// DeleteUserLog removes a user's security log (account deletion).
type DeleteUserLog struct{ UserID string }

type DeleteUserLogHandler struct{ Deps }

func (h DeleteUserLogHandler) Handle(ctx context.Context, cmd DeleteUserLog) error {
	return h.Events.DeleteByUser(ctx, cmd.UserID)
}

// DeleteTeamLog removes a team's audit log (team deletion).
type DeleteTeamLog struct{ TeamID string }

type DeleteTeamLogHandler struct{ Deps }

func (h DeleteTeamLogHandler) Handle(ctx context.Context, cmd DeleteTeamLog) error {
	return h.Events.DeleteByTeam(ctx, cmd.TeamID)
}
