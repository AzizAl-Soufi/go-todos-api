// Package mcpapi is the audit module's MCP transport: the team audit log and
// the caller's own security log. Each tool runs the same query as the
// matching REST endpoint and returns the same JSON shape.
package mcpapi

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/transport/httpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/mcpx"
)

type tools struct{ qry query.Handlers }

// Register adds the audit log tools to s.
func Register(s *mcp.Server, qry query.Handlers) {
	t := tools{qry: qry}

	mcpx.Add(s, &mcp.Tool{
		Name: "get_team_audit_log",
		Description: "Read a team's audit log, newest first: membership, role and invitation changes and changes to team projects, collections and todos, " +
			"each with who did it, when, and from which IP and user agent. Owners and admins only. " +
			"Returns a page of events and nextCursor (null on the last page). For the caller's own account activity, use get_security_log.",
		Annotations: mcpx.Read("Get team audit log"),
	}, t.teamLog)
	mcpx.Add(s, &mcp.Tool{
		Name: "get_security_log",
		Description: "Read the caller's own account security log, newest first: sign-ins, passkey changes, session revocations and similar events, " +
			"with IP and user agent. Returns a page of events and nextCursor (null on the last page). For a team's activity, use get_team_audit_log.",
		Annotations: mcpx.Read("Get security log"),
	}, t.userLog)
}

type pageInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"at most this many events, 1 to 200 (default 50)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"the nextCursor of the previous page; omit for the newest events"`
}

type teamLogInput struct {
	TeamID string `json:"teamId" jsonschema:"the team's ID"`
	pageInput
}

// request mirrors the REST ?limit= and ?cursor= parsing; 0 (omitted) is the
// default limit.
func (in pageInput) request() (query.PageRequest, error) {
	req := query.PageRequest{Limit: in.Limit}
	if in.Limit < 0 {
		return req, domain.ErrInvalidLimit
	}
	if in.Cursor != "" {
		c, err := domain.ParseCursor(in.Cursor)
		if err != nil {
			return req, err
		}
		req.Before = c
	}
	return req, nil
}

// toPage is httpapi.ToPage with details always an object: the inferred
// output schema doesn't allow null there, while REST may send null for an
// event without details.
func toPage(p query.Page) httpapi.PageResponse {
	out := httpapi.ToPage(p)
	for i := range out.Events {
		if out.Events[i].Details == nil {
			out.Events[i].Details = map[string]string{}
		}
	}
	return out
}

func (t tools) teamLog(ctx context.Context, a access.Actor, in teamLogInput) (httpapi.PageResponse, error) {
	page, err := in.request()
	if err != nil {
		return httpapi.PageResponse{}, err
	}
	p, err := t.qry.TeamLog.Handle(ctx, query.TeamLog{Actor: a, TeamID: in.TeamID, PageRequest: page})
	if err != nil {
		return httpapi.PageResponse{}, err
	}
	return toPage(p), nil
}

func (t tools) userLog(ctx context.Context, a access.Actor, in pageInput) (httpapi.PageResponse, error) {
	page, err := in.request()
	if err != nil {
		return httpapi.PageResponse{}, err
	}
	p, err := t.qry.UserLog.Handle(ctx, query.UserLog{UserID: a.UserID, PageRequest: page})
	if err != nil {
		return httpapi.PageResponse{}, err
	}
	return toPage(p), nil
}
