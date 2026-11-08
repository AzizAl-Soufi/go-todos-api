// Package memstore holds the in-memory todos, projects and collections
// repositories (local development and tests). Visibility is
// domain.Scope.Includes; the due-date alert scan needs the todos linked to
// their projects (Todos.LinkProjects). Every value crossing the lock
// boundary is a copy, and filtering and ordering reuse the domain's own
// definitions (domain.Query, domain.ProjectFilter).
package memstore

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// table is a map of records owned by a user or a team. id reads a record's
// ID, owner its creator and team (domain.Scope decides visibility), clone
// deep-copies a record.
type table[T any] struct {
	mu    sync.RWMutex
	data  map[string]*T
	id    func(*T) *string
	owner func(*T) (userID, teamID string)
	clone func(*T) *T
}

func newTable[T any](id func(*T) *string, owner func(*T) (string, string), clone func(*T) *T) *table[T] {
	return &table[T]{data: map[string]*T{}, id: id, owner: owner, clone: clone}
}

func (t *table[T]) visible(s domain.Scope, v *T) bool { return s.Includes(t.owner(v)) }

func (t *table[T]) personalOf(userID string) func(*T) bool {
	return func(v *T) bool { u, team := t.owner(v); return team == "" && u == userID }
}

func (t *table[T]) ofTeam(teamID string) func(*T) bool {
	return func(v *T) bool { _, team := t.owner(v); return team == teamID }
}

func (t *table[T]) create(v *T) {
	t.mu.Lock()
	defer t.mu.Unlock()
	*t.id(v) = bson.NewObjectID().Hex()
	t.data[*t.id(v)] = t.clone(v)
}

func (t *table[T]) get(s domain.Scope, id string) (*T, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	v, ok := t.data[id]
	if !ok || !t.visible(s, v) {
		return nil, apperrors.ErrNotFound
	}
	return t.clone(v), nil
}

func (t *table[T]) update(s domain.Scope, v *T) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	stored, ok := t.data[*t.id(v)]
	if !ok || !t.visible(s, stored) {
		return apperrors.ErrNotFound
	}
	t.data[*t.id(v)] = t.clone(v)
	return nil
}

func (t *table[T]) delete(s domain.Scope, id string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.data[id]
	if !ok || !t.visible(s, v) {
		return apperrors.ErrNotFound
	}
	delete(t.data, id)
	return nil
}

// list returns copies of the records in scope that match keep, ordered by cmp.
func (t *table[T]) list(s domain.Scope, keep func(*T) bool, cmp func(a, b *T) int) []*T {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*T, 0)
	for _, v := range t.data {
		if t.visible(s, v) && keep(v) {
			out = append(out, t.clone(v))
		}
	}
	slices.SortFunc(out, cmp)
	return out
}

// deleteWhere removes the records that match.
func (t *table[T]) deleteWhere(match func(*T) bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, v := range t.data {
		if match(v) {
			delete(t.data, id)
		}
	}
}

// updateWhere changes the records that match, in place.
func (t *table[T]) updateWhere(match func(*T) bool, change func(*T)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, v := range t.data {
		if match(v) {
			change(v)
		}
	}
}

func all[T any](*T) bool { return true }

func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := *t
	return &c
}

// ---- todos ----

type Todos struct {
	t *table[domain.Todo]

	// mu guards the due-date alert state, kept beside the todos (Update
	// never touches it), and the linked projects. Lock order: t.mu, then mu.
	mu       sync.Mutex
	sent     map[string]domain.DueAlertsSent
	projects *Projects
}

var _ application.TodoStore = (*Todos)(nil)

// NewTodos makes an empty store. Link it to the projects store with
// LinkProjects, so the due-date alert scan can leave out archived projects.
func NewTodos() *Todos {
	return &Todos{sent: map[string]domain.DueAlertsSent{}, t: newTable(
		func(v *domain.Todo) *string { return &v.ID },
		func(v *domain.Todo) (string, string) { return v.UserID, v.TeamID },
		func(v *domain.Todo) *domain.Todo {
			c := *v
			c.Tags = slices.Clone(v.Tags)
			c.DueAt, c.CompletedAt = copyTime(v.DueAt), copyTime(v.CompletedAt)
			return &c
		},
	)}
}

func (s *Todos) Create(_ context.Context, todo *domain.Todo) error {
	s.t.create(todo)
	return nil
}

func (s *Todos) Get(_ context.Context, sc domain.Scope, id string) (*domain.Todo, error) {
	return s.t.get(sc, id)
}

func (s *Todos) Update(_ context.Context, sc domain.Scope, todo *domain.Todo) error {
	return s.t.update(sc, todo)
}

func (s *Todos) Delete(_ context.Context, sc domain.Scope, id string) error {
	return s.t.delete(sc, id)
}

func (s *Todos) List(_ context.Context, sc domain.Scope, q domain.Query) ([]*domain.Todo, error) {
	return s.t.list(sc, q.Matches, q.Compare), nil
}

func (s *Todos) Progress(_ context.Context, sc domain.Scope, now time.Time) (map[string]domain.Progress, error) {
	out := map[string]domain.Progress{}
	for _, t := range s.t.list(sc, all, func(a, b *domain.Todo) int { return 0 }) {
		p := out[t.ProjectID]
		overdue := 0
		if t.Overdue(now) {
			overdue = 1
		}
		p.AddCount(t.Status, 1, overdue)
		out[t.ProjectID] = p
	}
	return out, nil
}

func (s *Todos) DeleteByProject(_ context.Context, sc domain.Scope, projectID string) error {
	s.t.deleteWhere(func(t *domain.Todo) bool { return t.ProjectID == projectID && s.t.visible(sc, t) })
	return nil
}

func (s *Todos) DeleteByUser(_ context.Context, userID string) error {
	s.t.deleteWhere(s.t.personalOf(userID))
	return nil
}

func (s *Todos) DeleteByTeam(_ context.Context, teamID string) error {
	s.t.deleteWhere(s.t.ofTeam(teamID))
	return nil
}

// LinkProjects lets the todos see their projects' archive state, which the
// due-date alert scan needs (the database stores join the two). Unlinked,
// no project counts as archived.
func (s *Todos) LinkProjects(p *Projects) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.projects = p
}

// ListPendingDueAlertsSystemWide is system-level: not scoped (see
// application.DueAlertRepository).
func (s *Todos) ListPendingDueAlertsSystemWide(_ context.Context, now time.Time, limit int) ([]application.DueAlert, error) {
	out := make([]application.DueAlert, 0)
	if limit <= 0 {
		return out, nil
	}
	s.t.mu.RLock()
	s.mu.Lock()
	for id := range s.sent { // forget the state of deleted todos
		if _, ok := s.t.data[id]; !ok {
			delete(s.sent, id)
		}
	}
	for id, t := range s.t.data {
		if kind, ok := domain.NextDueAlert(t, s.sent[id], now); ok {
			out = append(out, application.DueAlert{Todo: s.t.clone(t), Kind: kind})
		}
	}
	projects := s.projects
	s.mu.Unlock()
	s.t.mu.RUnlock()

	if projects != nil {
		out = slices.DeleteFunc(out, func(a application.DueAlert) bool { return projects.archived(a.Todo.ProjectID) })
	}
	slices.SortFunc(out, func(a, b application.DueAlert) int {
		if c := a.Todo.DueAt.Compare(*b.Todo.DueAt); c != 0 {
			return c
		}
		return strings.Compare(a.Todo.ID, b.Todo.ID)
	})
	return out[:min(limit, len(out))], nil
}

// MarkDueAlerted is system-level: not scoped (see
// application.DueAlertRepository).
func (s *Todos) MarkDueAlerted(_ context.Context, id string, kind domain.DueAlertKind, dueAt time.Time) (bool, error) {
	if kind != domain.AlertDueSoon && kind != domain.AlertOverdue {
		return false, fmt.Errorf("mark due alerted: unknown kind %q", kind)
	}
	s.t.mu.RLock()
	defer s.t.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.t.data[id]
	if !ok || !t.Status.Open() || t.DueAt == nil || !t.DueAt.Equal(dueAt) {
		return false, nil
	}
	sent := s.sent[id]
	if prev := sent.For(kind); prev != nil && prev.Equal(dueAt) {
		return false, nil
	}
	due := *t.DueAt
	if kind == domain.AlertOverdue {
		sent.OverdueFor = &due
	} else {
		sent.DueSoonFor = &due
	}
	s.sent[id] = sent
	return true, nil
}

// ---- projects ----

func cloneMeta(m domain.Meta) domain.Meta {
	m.ArchivedAt = copyTime(m.ArchivedAt)
	return m
}

func oldestFirst(a, b domain.Meta) int { return a.CreatedAt.Compare(b.CreatedAt) }

type Projects struct{ t *table[domain.Project] }

var _ application.ProjectStore = (*Projects)(nil)

func NewProjects() *Projects {
	return &Projects{t: newTable(
		func(v *domain.Project) *string { return &v.ID },
		func(v *domain.Project) (string, string) { return v.UserID, v.TeamID },
		func(v *domain.Project) *domain.Project { c := *v; c.Meta = cloneMeta(v.Meta); return &c },
	)}
}

func (s *Projects) Create(_ context.Context, p *domain.Project) error {
	s.t.create(p)
	return nil
}

func (s *Projects) Get(_ context.Context, sc domain.Scope, id string) (*domain.Project, error) {
	return s.t.get(sc, id)
}

func (s *Projects) Update(_ context.Context, sc domain.Scope, p *domain.Project) error {
	return s.t.update(sc, p)
}

func (s *Projects) Delete(_ context.Context, sc domain.Scope, id string) error {
	return s.t.delete(sc, id)
}

func (s *Projects) List(_ context.Context, sc domain.Scope, f domain.ProjectFilter) ([]*domain.Project, error) {
	return s.t.list(sc, f.Matches, func(a, b *domain.Project) int { return oldestFirst(a.Meta, b.Meta) }), nil
}

func (s *Projects) DetachCollection(_ context.Context, sc domain.Scope, collectionID string) error {
	s.t.updateWhere(
		func(p *domain.Project) bool { return p.CollectionID == collectionID && s.t.visible(sc, p) },
		func(p *domain.Project) { p.CollectionID = "" })
	return nil
}

// archived reports whether the project with id exists and is archived.
func (s *Projects) archived(id string) bool {
	if id == "" {
		return false
	}
	s.t.mu.RLock()
	defer s.t.mu.RUnlock()
	p, ok := s.t.data[id]
	return ok && p.Archived()
}

func (s *Projects) DeleteByUser(_ context.Context, userID string) error {
	s.t.deleteWhere(s.t.personalOf(userID))
	return nil
}

func (s *Projects) DeleteByTeam(_ context.Context, teamID string) error {
	s.t.deleteWhere(s.t.ofTeam(teamID))
	return nil
}

// ---- collections ----

type Collections struct{ t *table[domain.Collection] }

var _ application.CollectionStore = (*Collections)(nil)

func NewCollections() *Collections {
	return &Collections{t: newTable(
		func(v *domain.Collection) *string { return &v.ID },
		func(v *domain.Collection) (string, string) { return v.UserID, v.TeamID },
		func(v *domain.Collection) *domain.Collection { c := *v; c.Meta = cloneMeta(v.Meta); return &c },
	)}
}

func (s *Collections) Create(_ context.Context, c *domain.Collection) error {
	s.t.create(c)
	return nil
}

func (s *Collections) Get(_ context.Context, sc domain.Scope, id string) (*domain.Collection, error) {
	return s.t.get(sc, id)
}

func (s *Collections) Update(_ context.Context, sc domain.Scope, c *domain.Collection) error {
	return s.t.update(sc, c)
}

func (s *Collections) Delete(_ context.Context, sc domain.Scope, id string) error {
	return s.t.delete(sc, id)
}

func (s *Collections) List(_ context.Context, sc domain.Scope, f domain.CollectionFilter) ([]*domain.Collection, error) {
	return s.t.list(sc, f.Matches, func(a, b *domain.Collection) int { return oldestFirst(a.Meta, b.Meta) }), nil
}

func (s *Collections) DeleteByUser(_ context.Context, userID string) error {
	s.t.deleteWhere(s.t.personalOf(userID))
	return nil
}

func (s *Collections) DeleteByTeam(_ context.Context, teamID string) error {
	s.t.deleteWhere(s.t.ofTeam(teamID))
	return nil
}
