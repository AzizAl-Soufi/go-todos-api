// Package mcpx is the shared kernel for the modules' MCP transports: tool
// registration with the caller resolved, annotations, and errors turned
// into tool results the model can act on.
//
// Every tool runs behind the /mcp endpoint's middleware: an OAuth access
// token is verified, the account becomes an auth.Principal and
// access.Middleware adds the caller's team roles, exactly as for the REST
// API. Tools then call the same command and query handlers as the REST
// endpoints, so rules, audit and transactions are identical.
package mcpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
)

// Handler is a tool's logic: the caller and the decoded input in, the
// result out. Errors become tool errors (isError), not protocol errors.
type Handler[In, Out any] func(ctx context.Context, a access.Actor, in In) (Out, error)

// Add registers a typed tool. Input and output schemas are inferred from In
// and Out (descriptions from `jsonschema` struct tags); the input is
// validated before h runs.
func Add[In, Out any](s *mcp.Server, t *mcp.Tool, h Handler[In, Out]) {
	mcp.AddTool(s, t, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		a, ok := access.ActorFrom(ctx)
		if !ok {
			return nil, zero, errors.New("not signed in")
		}
		out, err := h(ctx, a, in)
		if err != nil {
			return nil, zero, toolError(ctx, t.Name, err)
		}
		return nil, out, nil
	})
}

// toolError describes err for the model: the API's stable error code and
// message, and when to retry if it says. Anything else is logged and
// reported as an internal error, so internals never leak.
func toolError(ctx context.Context, tool string, err error) error {
	ae, ok := apperrors.From(err)
	if !ok || ae.Status() >= 500 {
		slog.ErrorContext(ctx, "mcp tool failed", "tool", tool, "error", err)
		return errors.New("INTERNAL: the server failed to handle this request; try again later")
	}
	msg := fmt.Sprintf("%s: %s", ae.Code(), ae.Message())
	var later apperrors.RetryLaterError
	if errors.As(err, &later) {
		msg += fmt.Sprintf(" (retry at %s)", later.At.Format(time.RFC3339))
	}
	return errors.New(msg)
}

func ptr(b bool) *bool { return &b }

// Read annotates a tool without side effects: hosts may run it without
// asking.
func Read(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, DestructiveHint: ptr(false), IdempotentHint: true, OpenWorldHint: ptr(false)}
}

// Write annotates a tool that creates or changes records without
// destroying any.
func Write(title string, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: ptr(false), IdempotentHint: idempotent, OpenWorldHint: ptr(false)}
}

// Destructive annotates a tool that deletes or removes something: hosts
// ask before running it.
func Destructive(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: ptr(true), IdempotentHint: true, OpenWorldHint: ptr(false)}
}

// Outbound annotates a tool that reaches people outside this server (an
// invitation email).
func Outbound(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: ptr(false), OpenWorldHint: ptr(true)}
}

// Deleted is the result of a delete tool.
type Deleted struct {
	ID      string `json:"id" jsonschema:"the ID of what was deleted"`
	Deleted bool   `json:"deleted"`
}

type operationInput struct {
	ID string `json:"id" jsonschema:"the operation's ID, from the tool that started it"`
}

// RegisterOperations adds get_operation: the status of background work a
// tool started (deleting a team's content).
func RegisterOperations(s *mcp.Server, store jobs.Store) {
	Add(s, &mcp.Tool{
		Name:        "get_operation",
		Description: "Get the status of background work started by another tool (e.g. delete_team): pending, running, succeeded or failed.",
		Annotations: Read("Get operation status"),
	}, func(ctx context.Context, a access.Actor, in operationInput) (jobs.Operation, error) {
		j, err := store.Get(ctx, in.ID)
		if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrInvalidID) || err == nil && j.OwnerID != a.UserID {
			return jobs.Operation{}, jobs.ErrJobNotFound
		}
		if err != nil {
			return jobs.Operation{}, err
		}
		return jobs.ToOperation(j), nil
	})
}
