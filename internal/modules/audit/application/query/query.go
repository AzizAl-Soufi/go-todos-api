// Package query holds the audit module's read use cases: one page of a log,
// newest first, with a cursor for the next page.
package query

import (
	"context"
	"errors"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

type Deps struct {
	Events application.EventReader
}

type Handlers struct {
	TeamLog TeamLogHandler
	UserLog UserLogHandler
}

func New(d Deps) Handlers {
	return Handlers{TeamLog: TeamLogHandler{d}, UserLog: UserLogHandler{d}}
}

// PageRequest is where a page starts and how long it is (0 means the default).
type PageRequest struct {
	Before *domain.Cursor
	Limit  int
}

// Page is one page of events; Next is nil on the last page.
type Page struct {
	Events []*domain.Event
	Next   *domain.Cursor
}

// TeamLog reads a team's audit log; owners and admins only.
type TeamLog struct {
	Actor  access.Actor
	TeamID string
	PageRequest
}

type TeamLogHandler struct{ Deps }

func (h TeamLogHandler) Handle(ctx context.Context, q TeamLog) (Page, error) {
	if err := q.Actor.Require(q.TeamID, access.Role.CanManage); err != nil {
		return Page{}, err
	}
	return page(ctx, h.Events, domain.Filter{TeamID: q.TeamID}, q.PageRequest)
}

// UserLog reads a user's own security log.
type UserLog struct {
	UserID string
	PageRequest
}

type UserLogHandler struct{ Deps }

func (h UserLogHandler) Handle(ctx context.Context, q UserLog) (Page, error) {
	return page(ctx, h.Events, domain.Filter{UserID: q.UserID}, q.PageRequest)
}

// page reads one more event than asked for, to know whether a next page exists.
func page(ctx context.Context, events application.EventReader, f domain.Filter, req PageRequest) (Page, error) {
	limit := req.Limit
	switch {
	case limit == 0:
		limit = domain.DefaultLimit
	case limit < 0 || limit > domain.MaxLimit:
		return Page{}, domain.ErrInvalidLimit
	}
	f.Before, f.Limit = req.Before, limit+1
	list, err := events.List(ctx, f)
	if errors.Is(err, apperrors.ErrInvalidID) { // only the cursor can be malformed here
		return Page{}, domain.ErrInvalidCursor
	}
	if err != nil {
		return Page{}, err
	}
	p := Page{Events: list}
	if len(list) > limit {
		p.Events = list[:limit]
		next := domain.CursorOf(p.Events[limit-1])
		p.Next = &next
	}
	return p, nil
}
