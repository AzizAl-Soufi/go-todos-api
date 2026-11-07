// Package httpapi is the audit module's HTTP transport: the team audit log
// and the caller's own security log.
package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

// Register mounts the routes; protect must authenticate the caller and
// resolve them into an access.Actor.
func Register(mux *http.ServeMux, qry query.Handlers, protect httpx.Middleware) {
	h := handler{qry}
	mux.Handle("GET /teams/{id}/audit-log", protect(access.ForActor(h.teamLog)))
	mux.Handle("GET /me/audit-log", protect(access.ForActor(h.userLog)))
}

type handler struct{ qry query.Handlers }

type eventResponse struct {
	ID         string            `json:"id"`
	TeamID     *string           `json:"teamId"`
	UserID     *string           `json:"userId"`
	ActorID    *string           `json:"actorId"`
	Action     string            `json:"action"`
	TargetType *string           `json:"targetType"`
	TargetID   *string           `json:"targetId"`
	Details    map[string]string `json:"details"`
	IP         string            `json:"ip"`
	UserAgent  string            `json:"userAgent"`
	CreatedAt  time.Time         `json:"createdAt"`
}

type pageResponse struct {
	Events     []eventResponse `json:"events"`
	NextCursor *string         `json:"nextCursor"`
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toPage(p query.Page) pageResponse {
	out := pageResponse{Events: make([]eventResponse, 0, len(p.Events))}
	for _, e := range p.Events {
		out.Events = append(out.Events, eventResponse{
			ID: e.ID, TeamID: optional(e.TeamID), UserID: optional(e.UserID), ActorID: optional(e.ActorID),
			Action: e.Action, TargetType: optional(e.TargetType), TargetID: optional(e.TargetID),
			Details: e.Details, IP: e.IP, UserAgent: e.UserAgent, CreatedAt: e.CreatedAt,
		})
	}
	if p.Next != nil {
		out.NextCursor = optional(p.Next.String())
	}
	return out
}

// parsePage reads ?limit= and ?cursor=.
func parsePage(r *http.Request) (query.PageRequest, error) {
	var req query.PageRequest
	v := r.URL.Query()
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return req, domain.ErrInvalidLimit
		}
		req.Limit = n
	}
	if s := v.Get("cursor"); s != "" {
		c, err := domain.ParseCursor(s)
		if err != nil {
			return req, err
		}
		req.Before = c
	}
	return req, nil
}

func respond(p query.Page, err error) (int, any, error) {
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, toPage(p), nil
}

func (h handler) teamLog(r *http.Request, a access.Actor) (int, any, error) {
	page, err := parsePage(r)
	if err != nil {
		return 0, nil, err
	}
	return respond(h.qry.TeamLog.Handle(r.Context(), query.TeamLog{Actor: a, TeamID: r.PathValue("id"), PageRequest: page}))
}

func (h handler) userLog(r *http.Request, a access.Actor) (int, any, error) {
	page, err := parsePage(r)
	if err != nil {
		return 0, nil, err
	}
	return respond(h.qry.UserLog.Handle(r.Context(), query.UserLog{UserID: a.UserID, PageRequest: page}))
}
