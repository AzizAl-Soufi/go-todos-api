// Package pgstore holds the PostgreSQL todos, projects and collections
// repositories. Each call runs on the transaction carried by ctx (see
// platform/database/postgres.TxRunner) when there is one, otherwise on the pool.
package pgstore

import (
	"context"
	"strings"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/pgstore/sqlcgen"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type base struct{ pool *pgxpool.Pool }

func (b base) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Executor(ctx, b.pool))
}

// optionalUUID parses an ID that may be empty (no project, no collection).
func optionalUUID(id string) (*uuid.UUID, error) {
	if id == "" {
		return nil, nil
	}
	u, err := ids.UUID(id)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func idString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// ---- todos ----

type Todos struct{ base }

var _ application.TodoStore = (*Todos)(nil)

func NewTodos(pool *pgxpool.Pool) *Todos { return &Todos{base{pool}} }

func todoToDomain(t sqlcgen.Todo) *domain.Todo {
	return &domain.Todo{
		ID: t.ID.String(), UserID: t.UserID.String(), ProjectID: idString(t.ProjectID),
		Title: t.Title, Notes: t.Notes, Status: domain.Status(t.Status), Priority: domain.Priority(t.Priority),
		Tags: t.Tags, DueAt: t.DueAt, CompletedAt: t.CompletedAt, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

func nonNilTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}

func (s *Todos) Create(ctx context.Context, t *domain.Todo) error {
	userID, err := ids.UUID(t.UserID)
	if err != nil {
		return err
	}
	projectID, err := optionalUUID(t.ProjectID)
	if err != nil {
		return err
	}
	row, err := s.q(ctx).CreateTodo(ctx, sqlcgen.CreateTodoParams{
		UserID: userID, ProjectID: projectID, Title: t.Title, Notes: t.Notes, Status: string(t.Status),
		Priority: int16(t.Priority), Tags: nonNilTags(t.Tags), DueAt: t.DueAt, CompletedAt: t.CompletedAt,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	})
	if err != nil {
		return postgres.MapErr("create todo", err)
	}
	*t = *todoToDomain(row)
	return nil
}

func (s *Todos) Get(ctx context.Context, userID, id string) (*domain.Todo, error) {
	todoID, userUUID, err := ids.UUIDPair(id, userID)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetTodo(ctx, sqlcgen.GetTodoParams{ID: todoID, UserID: userUUID})
	if err != nil {
		return nil, postgres.MapErr("get todo", err)
	}
	return todoToDomain(row), nil
}

func (s *Todos) Update(ctx context.Context, t *domain.Todo) error {
	todoID, userID, err := ids.UUIDPair(t.ID, t.UserID)
	if err != nil {
		return err
	}
	projectID, err := optionalUUID(t.ProjectID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).UpdateTodo(ctx, sqlcgen.UpdateTodoParams{
		ID: todoID, UserID: userID, ProjectID: projectID, Title: t.Title, Notes: t.Notes,
		Status: string(t.Status), Priority: int16(t.Priority), Tags: nonNilTags(t.Tags),
		DueAt: t.DueAt, CompletedAt: t.CompletedAt, UpdatedAt: t.UpdatedAt,
	})
	return postgres.RequireAffected("update todo", rows, err)
}

func (s *Todos) Delete(ctx context.Context, userID, id string) error {
	todoID, userUUID, err := ids.UUIDPair(id, userID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteTodo(ctx, sqlcgen.DeleteTodoParams{ID: todoID, UserID: userUUID})
	return postgres.RequireAffected("delete todo", rows, err)
}

// likePattern turns a search term into an ILIKE pattern matching it anywhere.
func likePattern(s string) string {
	if s == "" {
		return ""
	}
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

func (s *Todos) List(ctx context.Context, userID string, q domain.Query) ([]*domain.Todo, error) {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return nil, err
	}
	projectID, err := optionalUUID(q.ProjectID)
	if err != nil {
		return nil, err
	}
	// pgx sends a nil slice as NULL, and "= ANY(NULL)" matches nothing, so
	// every array parameter is a non-nil slice.
	params := sqlcgen.ListTodosParams{
		UserID: userUUID, ProjectID: projectID, Inbox: q.Inbox,
		ExcludeProjects: []uuid.UUID{}, Statuses: []string{}, Priorities: []int16{},
		Tag: q.Tag, DueBefore: q.DueBefore, DueAfter: q.DueAfter, OverdueAt: q.OverdueAt,
		Search: likePattern(q.Search), Sort: string(q.Sort),
	}
	for _, id := range q.ExcludeProjectIDs {
		u, err := ids.UUID(id)
		if err != nil {
			return nil, err
		}
		params.ExcludeProjects = append(params.ExcludeProjects, u)
	}
	for _, st := range q.Statuses {
		params.Statuses = append(params.Statuses, string(st))
	}
	for _, p := range q.Priorities {
		params.Priorities = append(params.Priorities, int16(p))
	}

	rows, err := s.q(ctx).ListTodos(ctx, params)
	if err != nil {
		return nil, postgres.MapErr("list todos", err)
	}
	out := make([]*domain.Todo, 0, len(rows))
	for _, r := range rows {
		out = append(out, todoToDomain(r))
	}
	return out, nil
}

func (s *Todos) Progress(ctx context.Context, userID string, now time.Time) (map[string]domain.Progress, error) {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).TodoProgress(ctx, sqlcgen.TodoProgressParams{UserID: userUUID, Now: &now})
	if err != nil {
		return nil, postgres.MapErr("todo progress", err)
	}
	out := map[string]domain.Progress{}
	for _, r := range rows {
		key := idString(r.ProjectID)
		p := out[key]
		p.AddCount(domain.Status(r.Status), int(r.Total), int(r.Overdue))
		out[key] = p
	}
	return out, nil
}

func (s *Todos) DeleteByProject(ctx context.Context, userID, projectID string) error {
	userUUID, projectUUID, err := ids.UUIDPair(userID, projectID)
	if err != nil {
		return err
	}
	err = s.q(ctx).DeleteTodosByProject(ctx, sqlcgen.DeleteTodosByProjectParams{UserID: userUUID, ProjectID: &projectUUID})
	return postgres.MapErr("delete todos by project", err)
}

func (s *Todos) DeleteByUser(ctx context.Context, userID string) error {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete todos by user", s.q(ctx).DeleteTodosByUser(ctx, userUUID))
}

// ---- projects ----

type Projects struct{ base }

var _ application.ProjectStore = (*Projects)(nil)

func NewProjects(pool *pgxpool.Pool) *Projects { return &Projects{base{pool}} }

func projectToDomain(p sqlcgen.Project) *domain.Project {
	return &domain.Project{
		ID: p.ID.String(), UserID: p.UserID.String(), CollectionID: idString(p.CollectionID),
		Meta: domain.Meta{
			Name: p.Name, Description: p.Description, Icon: p.Icon, Color: p.Color,
			ArchivedAt: p.ArchivedAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		},
	}
}

func (s *Projects) Create(ctx context.Context, p *domain.Project) error {
	userID, err := ids.UUID(p.UserID)
	if err != nil {
		return err
	}
	collectionID, err := optionalUUID(p.CollectionID)
	if err != nil {
		return err
	}
	row, err := s.q(ctx).CreateProject(ctx, sqlcgen.CreateProjectParams{
		UserID: userID, CollectionID: collectionID, Name: p.Name, Description: p.Description, Icon: p.Icon,
		Color: p.Color, ArchivedAt: p.ArchivedAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	})
	if err != nil {
		return postgres.MapErr("create project", err)
	}
	*p = *projectToDomain(row)
	return nil
}

func (s *Projects) Get(ctx context.Context, userID, id string) (*domain.Project, error) {
	projectID, userUUID, err := ids.UUIDPair(id, userID)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetProject(ctx, sqlcgen.GetProjectParams{ID: projectID, UserID: userUUID})
	if err != nil {
		return nil, postgres.MapErr("get project", err)
	}
	return projectToDomain(row), nil
}

func (s *Projects) Update(ctx context.Context, p *domain.Project) error {
	projectID, userID, err := ids.UUIDPair(p.ID, p.UserID)
	if err != nil {
		return err
	}
	collectionID, err := optionalUUID(p.CollectionID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).UpdateProject(ctx, sqlcgen.UpdateProjectParams{
		ID: projectID, UserID: userID, CollectionID: collectionID, Name: p.Name, Description: p.Description,
		Icon: p.Icon, Color: p.Color, ArchivedAt: p.ArchivedAt, UpdatedAt: p.UpdatedAt,
	})
	return postgres.RequireAffected("update project", rows, err)
}

func (s *Projects) Delete(ctx context.Context, userID, id string) error {
	projectID, userUUID, err := ids.UUIDPair(id, userID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteProject(ctx, sqlcgen.DeleteProjectParams{ID: projectID, UserID: userUUID})
	return postgres.RequireAffected("delete project", rows, err)
}

func (s *Projects) List(ctx context.Context, userID string, f domain.ProjectFilter) ([]*domain.Project, error) {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return nil, err
	}
	collectionID, err := optionalUUID(f.CollectionID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).ListProjects(ctx, sqlcgen.ListProjectsParams{UserID: userUUID, CollectionID: collectionID, Archive: int32(f.Archive)})
	if err != nil {
		return nil, postgres.MapErr("list projects", err)
	}
	out := make([]*domain.Project, 0, len(rows))
	for _, r := range rows {
		out = append(out, projectToDomain(r))
	}
	return out, nil
}

func (s *Projects) DetachCollection(ctx context.Context, userID, collectionID string) error {
	userUUID, collectionUUID, err := ids.UUIDPair(userID, collectionID)
	if err != nil {
		return err
	}
	err = s.q(ctx).DetachCollection(ctx, sqlcgen.DetachCollectionParams{UserID: userUUID, CollectionID: &collectionUUID})
	return postgres.MapErr("detach collection", err)
}

func (s *Projects) DeleteByUser(ctx context.Context, userID string) error {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete projects by user", s.q(ctx).DeleteProjectsByUser(ctx, userUUID))
}

// ---- collections ----

type Collections struct{ base }

var _ application.CollectionStore = (*Collections)(nil)

func NewCollections(pool *pgxpool.Pool) *Collections { return &Collections{base{pool}} }

func collectionToDomain(c sqlcgen.Collection) *domain.Collection {
	return &domain.Collection{
		ID: c.ID.String(), UserID: c.UserID.String(),
		Meta: domain.Meta{
			Name: c.Name, Description: c.Description, Icon: c.Icon, Color: c.Color,
			ArchivedAt: c.ArchivedAt, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		},
	}
}

func (s *Collections) Create(ctx context.Context, c *domain.Collection) error {
	userID, err := ids.UUID(c.UserID)
	if err != nil {
		return err
	}
	row, err := s.q(ctx).CreateCollection(ctx, sqlcgen.CreateCollectionParams{
		UserID: userID, Name: c.Name, Description: c.Description, Icon: c.Icon, Color: c.Color,
		ArchivedAt: c.ArchivedAt, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	})
	if err != nil {
		return postgres.MapErr("create collection", err)
	}
	*c = *collectionToDomain(row)
	return nil
}

func (s *Collections) Get(ctx context.Context, userID, id string) (*domain.Collection, error) {
	collectionID, userUUID, err := ids.UUIDPair(id, userID)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetCollection(ctx, sqlcgen.GetCollectionParams{ID: collectionID, UserID: userUUID})
	if err != nil {
		return nil, postgres.MapErr("get collection", err)
	}
	return collectionToDomain(row), nil
}

func (s *Collections) Update(ctx context.Context, c *domain.Collection) error {
	collectionID, userID, err := ids.UUIDPair(c.ID, c.UserID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).UpdateCollection(ctx, sqlcgen.UpdateCollectionParams{
		ID: collectionID, UserID: userID, Name: c.Name, Description: c.Description, Icon: c.Icon,
		Color: c.Color, ArchivedAt: c.ArchivedAt, UpdatedAt: c.UpdatedAt,
	})
	return postgres.RequireAffected("update collection", rows, err)
}

func (s *Collections) Delete(ctx context.Context, userID, id string) error {
	collectionID, userUUID, err := ids.UUIDPair(id, userID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteCollection(ctx, sqlcgen.DeleteCollectionParams{ID: collectionID, UserID: userUUID})
	return postgres.RequireAffected("delete collection", rows, err)
}

func (s *Collections) List(ctx context.Context, userID string, f domain.ArchiveFilter) ([]*domain.Collection, error) {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).ListCollections(ctx, sqlcgen.ListCollectionsParams{UserID: userUUID, Archive: int32(f)})
	if err != nil {
		return nil, postgres.MapErr("list collections", err)
	}
	out := make([]*domain.Collection, 0, len(rows))
	for _, r := range rows {
		out = append(out, collectionToDomain(r))
	}
	return out, nil
}

func (s *Collections) DeleteByUser(ctx context.Context, userID string) error {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete collections by user", s.q(ctx).DeleteCollectionsByUser(ctx, userUUID))
}
