// Package memstore holds the in-memory todos, projects and collections
// repositories (local development and tests). Every value crossing the lock
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

// table is a map of user-owned records. id and owner read a record's ID and
// owner; clone deep-copies a record.
type table[T any] struct {
	mu    sync.RWMutex
	data  map[string]*T
	id    func(*T) *string
	owner func(*T) string
	clone func(*T) *T
}

func newTable[T any](id func(*T) *string, owner func(*T) string, clone func(*T) *T) *table[T] {
	return &table[T]{data: map[string]*T{}, id: id, owner: owner, clone: clone}
}

func (t *table[T]) create(v *T) {
	t.mu.Lock()
	defer t.mu.Unlock()
	*t.id(v) = bson.NewObjectID().Hex()
	t.data[*t.id(v)] = t.clone(v)
}

func (t *table[T]) get(userID, id string) (*T, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	v, ok := t.data[id]
	if !ok || t.owner(v) != userID {
		return nil, apperrors.ErrNotFound
	}
	return t.clone(v), nil
}

func (t *table[T]) update(v *T) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	stored, ok := t.data[*t.id(v)]
	if !ok || t.owner(stored) != t.owner(v) {
		return apperrors.ErrNotFound
	}
	t.data[*t.id(v)] = t.clone(v)
	return nil
}

func (t *table[T]) delete(userID, id string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.data[id]
	if !ok || t.owner(v) != userID {
		return apperrors.ErrNotFound
	}
	delete(t.data, id)
	return nil
}

// list returns copies of the user's records that match keep, ordered by cmp.
func (t *table[T]) list(userID string, keep func(*T) bool, cmp func(a, b *T) int) []*T {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*T, 0)
	for _, v := range t.data {
		if t.owner(v) == userID && keep(v) {
			out = append(out, t.clone(v))
		}
	}
	slices.SortFunc(out, cmp)
	return out
}

// deleteWhere removes the user's records that match.
func (t *table[T]) deleteWhere(userID string, match func(*T) bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, v := range t.data {
		if t.owner(v) == userID && match(v) {
			delete(t.data, id)
		}
	}
}

// updateWhere changes the user's records that match, in place.
func (t *table[T]) updateWhere(userID string, match func(*T) bool, change func(*T)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, v := range t.data {
		if t.owner(v) == userID && match(v) {
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
		func(v *domain.Todo) string { return v.UserID },
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

func (s *Todos) Get(_ context.Context, userID, id string) (*domain.Todo, error) {
	return s.t.get(userID, id)
}

func (s *Todos) Update(_ context.Context, todo *domain.Todo) error { return s.t.update(todo) }

func (s *Todos) Delete(_ context.Context, userID, id string) error { return s.t.delete(userID, id) }

func (s *Todos) List(_ context.Context, userID string, q domain.Query) ([]*domain.Todo, error) {
	return s.t.list(userID, q.Matches, q.Compare), nil
}

func (s *Todos) Progress(_ context.Context, userID string, now time.Time) (map[string]domain.Progress, error) {
	out := map[string]domain.Progress{}
	for _, t := range s.t.list(userID, all, func(a, b *domain.Todo) int { return 0 }) {
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

func (s *Todos) DeleteByProject(_ context.Context, userID, projectID string) error {
	s.t.deleteWhere(userID, func(t *domain.Todo) bool { return t.ProjectID == projectID })
	return nil
}

func (s *Todos) DeleteByUser(_ context.Context, userID string) error {
	s.t.deleteWhere(userID, all)
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
		func(v *domain.Project) string { return v.UserID },
		func(v *domain.Project) *domain.Project { c := *v; c.Meta = cloneMeta(v.Meta); return &c },
	)}
}

func (s *Projects) Create(_ context.Context, p *domain.Project) error {
	s.t.create(p)
	return nil
}

func (s *Projects) Get(_ context.Context, userID, id string) (*domain.Project, error) {
	return s.t.get(userID, id)
}

func (s *Projects) Update(_ context.Context, p *domain.Project) error { return s.t.update(p) }

func (s *Projects) Delete(_ context.Context, userID, id string) error { return s.t.delete(userID, id) }

func (s *Projects) List(_ context.Context, userID string, f domain.ProjectFilter) ([]*domain.Project, error) {
	return s.t.list(userID, f.Matches, func(a, b *domain.Project) int { return oldestFirst(a.Meta, b.Meta) }), nil
}

func (s *Projects) DetachCollection(_ context.Context, userID, collectionID string) error {
	s.t.updateWhere(userID,
		func(p *domain.Project) bool { return p.CollectionID == collectionID },
		func(p *domain.Project) { p.CollectionID = "" })
	return nil
}

func (s *Projects) DeleteByUser(_ context.Context, userID string) error {
	s.t.deleteWhere(userID, all)
	return nil
}

// ---- collections ----

type Collections struct{ t *table[domain.Collection] }

var _ application.CollectionStore = (*Collections)(nil)

func NewCollections() *Collections {
	return &Collections{t: newTable(
		func(v *domain.Collection) *string { return &v.ID },
		func(v *domain.Collection) string { return v.UserID },
		func(v *domain.Collection) *domain.Collection { c := *v; c.Meta = cloneMeta(v.Meta); return &c },
	)}
}

func (s *Collections) Create(_ context.Context, c *domain.Collection) error {
	s.t.create(c)
	return nil
}

func (s *Collections) Get(_ context.Context, userID, id string) (*domain.Collection, error) {
	return s.t.get(userID, id)
}

func (s *Collections) Update(_ context.Context, c *domain.Collection) error { return s.t.update(c) }

func (s *Collections) Delete(_ context.Context, userID, id string) error {
	return s.t.delete(userID, id)
}

func (s *Collections) List(_ context.Context, userID string, f domain.ArchiveFilter) ([]*domain.Collection, error) {
	keep := func(c *domain.Collection) bool { return f.Matches(c.Meta) }
	return s.t.list(userID, keep, func(a, b *domain.Collection) int { return oldestFirst(a.Meta, b.Meta) }), nil
}

func (s *Collections) DeleteByUser(_ context.Context, userID string) error {
	s.t.deleteWhere(userID, all)
	return nil
}
