// Package memstore holds the in-memory todos, projects and collections
// repositories (local development and tests). Visibility is domain.Scope.Includes. Every value crossing the lock
// boundary is a copy, and filtering and ordering reuse the domain's own
// definitions (domain.Query, domain.ProjectFilter).
package memstore

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"go.mongodb.org/mongo-driver/v2/bson"
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

type Todos struct{ t *table[domain.Todo] }

var _ application.TodoStore = (*Todos)(nil)

func NewTodos() *Todos {
	return &Todos{t: newTable(
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
