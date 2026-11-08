package jobs

import (
	"errors"
	"net/http"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

// Operation is a background job as the user who started it sees it: what
// it is and how far along. Internals (attempts, errors) stay in the logs.
type Operation struct {
	ID         string     `json:"id" jsonschema:"the operation's ID"`
	Kind       string     `json:"kind" jsonschema:"what it does, e.g. teams.purge"`
	Status     string     `json:"status" jsonschema:"pending, running, succeeded or failed"`
	URL        string     `json:"url" jsonschema:"where to read its status"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	FinishedAt *time.Time `json:"finishedAt" jsonschema:"when it succeeded or failed; null until then"`
}

func operationURL(id string) string { return "/operations/" + id }

// ToOperation shows a job as an operation.
func ToOperation(j *Job) Operation {
	op := Operation{
		ID: j.ID, Kind: j.Kind, URL: operationURL(j.ID), CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
	switch j.Status {
	case Queued:
		op.Status = "pending"
	case Running, Dispatched:
		op.Status = "running"
	default:
		op.Status = string(j.Status)
	}
	if !j.FinishedAt.IsZero() {
		f := j.FinishedAt
		op.FinishedAt = &f
	}
	return op
}

// Accepted is the 202 response body for a request whose work continues in
// the background as the job id.
type Accepted struct {
	Operation struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		URL    string `json:"url"`
	} `json:"operation"`
}

func NewAccepted(id string) Accepted {
	var a Accepted
	a.Operation.ID, a.Operation.Status, a.Operation.URL = id, "pending", operationURL(id)
	return a
}

// RegisterOperations mounts GET /operations/{id}: the status of a job the
// caller started. Anyone else's job, a system job, and a job cleaned up
// after its retention look the same: OPERATION_NOT_FOUND.
func RegisterOperations(mux *http.ServeMux, store Store, protect httpx.Middleware) {
	mux.Handle("GET /operations/{id}", protect(auth.ForUser(func(r *http.Request, userID string) (int, any, error) {
		j, err := store.Get(r.Context(), r.PathValue("id"))
		if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrInvalidID) || err == nil && j.OwnerID != userID {
			return 0, nil, ErrJobNotFound
		}
		if err != nil {
			return 0, nil, err
		}
		return http.StatusOK, ToOperation(j), nil
	})))
}
