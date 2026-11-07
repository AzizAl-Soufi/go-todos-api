package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

type todoResponse struct {
	ID          string     `json:"id"`
	UserID      string     `json:"userId"`
	TeamID      *string    `json:"teamId"`
	ProjectID   *string    `json:"projectId"`
	Title       string     `json:"title"`
	Notes       string     `json:"notes"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`
	Tags        []string   `json:"tags"`
	DueAt       *time.Time `json:"dueAt"`
	Overdue     bool       `json:"overdue"`
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// toTodo renders a todo; "overdue" uses the query side's clock.
func (h handler) toTodo(t *domain.Todo) todoResponse {
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	return todoResponse{
		ID: t.ID, UserID: t.UserID, TeamID: optionalID(t.TeamID), ProjectID: optionalID(t.ProjectID),
		Title: t.Title, Notes: t.Notes, Status: string(t.Status), Priority: t.Priority.String(), Tags: tags,
		DueAt: t.DueAt, Overdue: t.Overdue(h.qry.Now()), Completed: t.Completed(), CompletedAt: t.CompletedAt,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// decodeTodo reads the todo fields; create and update share them.
func decodeTodo(r *http.Request, requireFields bool) (domain.Changes, error) {
	b, err := decodeBody(r, requireFields)
	if err != nil {
		return domain.Changes{}, err
	}
	var c domain.Changes
	var errs [8]error
	c.Title, errs[0] = b.str("title", domain.ErrInvalidTitle)
	c.Notes, errs[1] = b.str("notes", nil)
	c.Status, errs[2] = b.status("status")
	c.Priority, errs[3] = b.priority("priority")
	c.Tags, errs[4] = b.strings("tags")
	c.DueAt, errs[5] = b.time("dueAt")
	c.ProjectID, errs[6] = b.id("projectId")
	c.Completed, errs[7] = b.boolean("completed", apperrors.ErrInvalidRequestValue)
	return c, firstErr(errs[:]...)
}

func (h handler) readTodo(r *http.Request, status int, a access.Actor, id string) (int, any, error) {
	todo, err := h.qry.GetTodo.Handle(r.Context(), query.GetTodo{Actor: a, ID: id})
	return respond(status, todo, err, h.toTodo)
}

func (h handler) createTodo(r *http.Request, a access.Actor) (int, any, error) {
	c, err := decodeTodo(r, false)
	if err != nil {
		return 0, nil, err
	}
	id, err := h.cmd.CreateTodo.Handle(r.Context(), command.CreateTodo{Actor: a, Changes: c})
	if err != nil {
		return 0, nil, err
	}
	return h.readTodo(r, http.StatusCreated, a, id)
}

func (h handler) getTodo(r *http.Request, a access.Actor) (int, any, error) {
	return h.readTodo(r, http.StatusOK, a, r.PathValue("id"))
}

func (h handler) updateTodo(r *http.Request, a access.Actor) (int, any, error) {
	c, err := decodeTodo(r, true)
	if err != nil {
		return 0, nil, err
	}
	id := r.PathValue("id")
	if err := h.cmd.UpdateTodo.Handle(r.Context(), command.UpdateTodo{Actor: a, ID: id, Changes: c}); err != nil {
		return 0, nil, err
	}
	return h.readTodo(r, http.StatusOK, a, id)
}

func (h handler) deleteTodo(r *http.Request, a access.Actor) (int, any, error) {
	err := h.cmd.DeleteTodo.Handle(r.Context(), command.DeleteTodo{Actor: a, ID: r.PathValue("id")})
	return http.StatusOK, map[string]any{}, err
}

func (h handler) listTodos(r *http.Request, a access.Actor) (int, any, error) {
	q, err := parseListTodos(r.URL.Query())
	if err != nil {
		return 0, nil, err
	}
	q.Actor = a
	todos, err := h.qry.ListTodos.Handle(r.Context(), q)
	return respondList(todos, err, h.toTodo)
}

// parseOwner reads ?team=: a team ID, or "personal" for personal records only.
func parseOwner(v url.Values) domain.OwnerFilter {
	switch team := v.Get("team"); team {
	case "":
		return domain.OwnerFilter{}
	case "personal":
		return domain.OwnerFilter{Personal: true}
	default:
		return domain.OwnerFilter{TeamID: team}
	}
}

func invalidQuery(param, want string) error {
	return apperrors.Validation("INVALID_QUERY", param+" must be "+want)
}

// parseListTodos reads the GET /todos query string.
func parseListTodos(v url.Values) (query.ListTodos, error) {
	q := query.ListTodos{Query: domain.Query{OwnerFilter: parseOwner(v)}}
	switch project := v.Get("project"); project {
	case "":
	case "inbox":
		q.Inbox = true
	default:
		q.ProjectID = project
	}
	var err error
	if q.Statuses, err = parseEach(v.Get("status"), domain.ParseStatus); err != nil {
		return q, err
	}
	if q.Priorities, err = parseEach(v.Get("priority"), domain.ParsePriority); err != nil {
		return q, err
	}
	if q.DueBefore, err = parseTimeParam(v, "due_before"); err != nil {
		return q, err
	}
	if q.DueAfter, err = parseTimeParam(v, "due_after"); err != nil {
		return q, err
	}
	if q.Overdue, err = parseBoolParam(v, "overdue"); err != nil {
		return q, err
	}
	if q.IncludeArchived, err = parseBoolParam(v, "include_archived"); err != nil {
		return q, err
	}
	q.Tag = strings.TrimSpace(v.Get("tag"))
	q.Search = strings.TrimSpace(v.Get("q"))
	q.Sort, err = domain.ParseSort(v.Get("sort"))
	return q, err
}

// parseEach parses a comma-separated list.
func parseEach[T any](list string, parse func(string) (T, error)) ([]T, error) {
	var out []T
	for part := range strings.SplitSeq(list, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		v, err := parse(part)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func parseTimeParam(v url.Values, param string) (*time.Time, error) {
	s := v.Get(param)
	if s == "" {
		return nil, nil
	}
	t, err := parseTime(s)
	if err != nil {
		return nil, invalidQuery(param, "an RFC 3339 timestamp or a YYYY-MM-DD date")
	}
	return &t, nil
}

func parseBoolParam(v url.Values, param string) (bool, error) {
	s := v.Get(param)
	if s == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, invalidQuery(param, "true or false")
	}
	return b, nil
}
