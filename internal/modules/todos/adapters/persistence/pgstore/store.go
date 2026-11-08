// Package pgstore holds the PostgreSQL todos, projects and collections
// repositories. Each call runs on the transaction carried by ctx (see
// platform/database/postgres.TxRunner) when there is one, otherwise on the pool.
package pgstore

import (
	"context"
	"fmt"
	"math"
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

// optionalUUID parses an ID that may be empty (no team, project, collection).
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

// scope is a parsed domain.Scope. The team list is never nil: pgx sends a nil
// slice as NULL, and "= ANY(NULL)" is NULL rather than false.
type scope struct {
	user  uuid.UUID
	teams []uuid.UUID
}

func parseScope(s domain.Scope) (scope, error) {
	user, err := ids.UUID(s.UserID)
	if err != nil {
		return scope{}, err
	}
	out := scope{user: user, teams: make([]uuid.UUID, 0, len(s.TeamIDs))}
	for _, id := range s.TeamIDs {
		u, err := ids.UUID(id)
		if err != nil {
			return scope{}, err
		}
		out.teams = append(out.teams, u)
	}
	return out, nil
}

// scopeAndID parses a scope with the ID of a record in it.
func scopeAndID(s domain.Scope, id string) (scope, uuid.UUID, error) {
	recordID, err := ids.UUID(id)
	if err != nil {
		return scope{}, uuid.Nil, err
	}
	sc, err := parseScope(s)
	return sc, recordID, err
}

// ---- todos ----

type Todos struct{ base }

var _ application.TodoStore = (*Todos)(nil)

func NewTodos(pool *pgxpool.Pool) *Todos { return &Todos{base{pool}} }

func todoToDomain(t sqlcgen.Todo) *domain.Todo {
	return &domain.Todo{
		ID: t.ID.String(), UserID: t.UserID.String(), TeamID: idString(t.TeamID), ProjectID: idString(t.ProjectID),
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

// todoRefs parses the IDs a todo row stores.
func todoRefs(t *domain.Todo) (user uuid.UUID, team, project *uuid.UUID, err error) {
	if user, err = ids.UUID(t.UserID); err != nil {
		return
	}
	if team, err = optionalUUID(t.TeamID); err != nil {
		return
	}
	project, err = optionalUUID(t.ProjectID)
	return
}

func (s *Todos) Create(ctx context.Context, t *domain.Todo) error {
	userID, teamID, projectID, err := todoRefs(t)
	if err != nil {
		return err
	}
	row, err := s.q(ctx).CreateTodo(ctx, sqlcgen.CreateTodoParams{
		UserID: userID, TeamID: teamID, ProjectID: projectID, Title: t.Title, Notes: t.Notes, Status: string(t.Status),
		Priority: int16(t.Priority), Tags: nonNilTags(t.Tags), DueAt: t.DueAt, CompletedAt: t.CompletedAt,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	})
	if err != nil {
		return postgres.MapErr("create todo", err)
	}
	*t = *todoToDomain(row)
	return nil
}

func (s *Todos) Get(ctx context.Context, sc domain.Scope, id string) (*domain.Todo, error) {
	p, todoID, err := scopeAndID(sc, id)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetTodo(ctx, sqlcgen.GetTodoParams{ID: todoID, ScopeUser: p.user, ScopeTeams: p.teams})
	if err != nil {
		return nil, postgres.MapErr("get todo", err)
	}
	return todoToDomain(row), nil
}

func (s *Todos) Update(ctx context.Context, sc domain.Scope, t *domain.Todo) error {
	p, todoID, err := scopeAndID(sc, t.ID)
	if err != nil {
		return err
	}
	userID, teamID, projectID, err := todoRefs(t)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).UpdateTodo(ctx, sqlcgen.UpdateTodoParams{
		ID: todoID, ScopeUser: p.user, ScopeTeams: p.teams, UserID: userID, TeamID: teamID, ProjectID: projectID,
		Title: t.Title, Notes: t.Notes, Status: string(t.Status), Priority: int16(t.Priority), Tags: nonNilTags(t.Tags),
		DueAt: t.DueAt, CompletedAt: t.CompletedAt, UpdatedAt: t.UpdatedAt,
	})
	return postgres.RequireAffected("update todo", rows, err)
}

func (s *Todos) Delete(ctx context.Context, sc domain.Scope, id string) error {
	p, todoID, err := scopeAndID(sc, id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteTodo(ctx, sqlcgen.DeleteTodoParams{ID: todoID, ScopeUser: p.user, ScopeTeams: p.teams})
	return postgres.RequireAffected("delete todo", rows, err)
}

// likePattern turns a search term into an ILIKE pattern matching it anywhere.
func likePattern(s string) string {
	if s == "" {
		return ""
	}
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

func (s *Todos) List(ctx context.Context, sc domain.Scope, q domain.Query) ([]*domain.Todo, error) {
	p, err := parseScope(sc)
	if err != nil {
		return nil, err
	}
	teamID, err := optionalUUID(q.TeamID)
	if err != nil {
		return nil, err
	}
	projectID, err := optionalUUID(q.ProjectID)
	if err != nil {
		return nil, err
	}
	// Every array parameter is a non-nil slice (see scope).
	params := sqlcgen.ListTodosParams{
		ScopeUser: p.user, ScopeTeams: p.teams, Personal: q.Personal, TeamID: teamID,
		ProjectID: projectID, Inbox: q.Inbox,
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
	for _, pr := range q.Priorities {
		params.Priorities = append(params.Priorities, int16(pr))
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

func (s *Todos) Progress(ctx context.Context, sc domain.Scope, now time.Time) (map[string]domain.Progress, error) {
	p, err := parseScope(sc)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).TodoProgress(ctx, sqlcgen.TodoProgressParams{ScopeUser: p.user, ScopeTeams: p.teams, Now: &now})
	if err != nil {
		return nil, postgres.MapErr("todo progress", err)
	}
	out := map[string]domain.Progress{}
	for _, r := range rows {
		key := idString(r.ProjectID)
		pr := out[key]
		pr.AddCount(domain.Status(r.Status), int(r.Total), int(r.Overdue))
		out[key] = pr
	}
	return out, nil
}

func (s *Todos) DeleteByProject(ctx context.Context, sc domain.Scope, projectID string) error {
	p, projectUUID, err := scopeAndID(sc, projectID)
	if err != nil {
		return err
	}
	err = s.q(ctx).DeleteTodosByProject(ctx, sqlcgen.DeleteTodosByProjectParams{ProjectID: &projectUUID, ScopeUser: p.user, ScopeTeams: p.teams})
	return postgres.MapErr("delete todos by project", err)
}

func (s *Todos) DeleteByUser(ctx context.Context, userID string) error {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete todos by user", s.q(ctx).DeleteTodosByUser(ctx, userUUID))
}

func (s *Todos) DeleteByTeam(ctx context.Context, teamID string) error {
	teamUUID, err := ids.UUID(teamID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete todos by team", s.q(ctx).DeleteTodosByTeam(ctx, &teamUUID))
}

// ListPendingDueAlertsSystemWide is system-level: not scoped (see
// application.DueAlertRepository).
func (s *Todos) ListPendingDueAlertsSystemWide(ctx context.Context, now time.Time, limit int) ([]application.DueAlert, error) {
	if limit <= 0 {
		return []application.DueAlert{}, nil
	}
	rows, err := s.q(ctx).ListPendingDueAlerts(ctx, sqlcgen.ListPendingDueAlertsParams{
		Now: now, DueSoonUntil: now.Add(domain.DueSoonWindow), MaxRows: int32(min(limit, math.MaxInt32)),
	})
	if err != nil {
		return nil, postgres.MapErr("list pending due alerts", err)
	}
	out := make([]application.DueAlert, 0, len(rows))
	for _, r := range rows {
		kind := domain.AlertDueSoon
		if r.Overdue {
			kind = domain.AlertOverdue
		}
		todo := todoToDomain(sqlcgen.Todo{
			ID: r.ID, UserID: r.UserID, TeamID: r.TeamID, ProjectID: r.ProjectID, Title: r.Title, Notes: r.Notes,
			Status: r.Status, Priority: r.Priority, Tags: r.Tags, DueAt: r.DueAt, CompletedAt: r.CompletedAt,
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		})
		out = append(out, application.DueAlert{Todo: todo, Kind: kind})
	}
	return out, nil
}

// MarkDueAlerted is system-level: not scoped (see
// application.DueAlertRepository).
func (s *Todos) MarkDueAlerted(ctx context.Context, id string, kind domain.DueAlertKind, dueAt time.Time) (bool, error) {
	todoID, err := ids.UUID(id)
	if err != nil {
		return false, err
	}
	var rows int64
	switch kind {
	case domain.AlertDueSoon:
		rows, err = s.q(ctx).MarkDueSoonAlerted(ctx, sqlcgen.MarkDueSoonAlertedParams{ID: todoID, DueAt: dueAt})
	case domain.AlertOverdue:
		rows, err = s.q(ctx).MarkOverdueAlerted(ctx, sqlcgen.MarkOverdueAlertedParams{ID: todoID, DueAt: dueAt})
	default:
		return false, fmt.Errorf("mark due alerted: unknown kind %q", kind)
	}
	if err != nil {
		return false, postgres.MapErr("mark due alerted", err)
	}
	return rows > 0, nil
}

// ---- projects ----

type Projects struct{ base }

var _ application.ProjectStore = (*Projects)(nil)

func NewProjects(pool *pgxpool.Pool) *Projects { return &Projects{base{pool}} }

func metaToDomain(name, description, icon, color string, archivedAt *time.Time, createdAt, updatedAt time.Time) domain.Meta {
	return domain.Meta{
		Name: name, Description: description, Icon: icon, Color: color,
		ArchivedAt: archivedAt, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
}

func projectToDomain(p sqlcgen.Project) *domain.Project {
	return &domain.Project{
		ID: p.ID.String(), UserID: p.UserID.String(), TeamID: idString(p.TeamID), CollectionID: idString(p.CollectionID),
		Meta: metaToDomain(p.Name, p.Description, p.Icon, p.Color, p.ArchivedAt, p.CreatedAt, p.UpdatedAt),
	}
}

func (s *Projects) Create(ctx context.Context, p *domain.Project) error {
	userID, err := ids.UUID(p.UserID)
	if err != nil {
		return err
	}
	teamID, err := optionalUUID(p.TeamID)
	if err != nil {
		return err
	}
	collectionID, err := optionalUUID(p.CollectionID)
	if err != nil {
		return err
	}
	row, err := s.q(ctx).CreateProject(ctx, sqlcgen.CreateProjectParams{
		UserID: userID, TeamID: teamID, CollectionID: collectionID, Name: p.Name, Description: p.Description,
		Icon: p.Icon, Color: p.Color, ArchivedAt: p.ArchivedAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	})
	if err != nil {
		return postgres.MapErr("create project", err)
	}
	*p = *projectToDomain(row)
	return nil
}

func (s *Projects) Get(ctx context.Context, sc domain.Scope, id string) (*domain.Project, error) {
	p, projectID, err := scopeAndID(sc, id)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetProject(ctx, sqlcgen.GetProjectParams{ID: projectID, ScopeUser: p.user, ScopeTeams: p.teams})
	if err != nil {
		return nil, postgres.MapErr("get project", err)
	}
	return projectToDomain(row), nil
}

func (s *Projects) Update(ctx context.Context, sc domain.Scope, pr *domain.Project) error {
	p, projectID, err := scopeAndID(sc, pr.ID)
	if err != nil {
		return err
	}
	collectionID, err := optionalUUID(pr.CollectionID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).UpdateProject(ctx, sqlcgen.UpdateProjectParams{
		ID: projectID, ScopeUser: p.user, ScopeTeams: p.teams, CollectionID: collectionID, Name: pr.Name,
		Description: pr.Description, Icon: pr.Icon, Color: pr.Color, ArchivedAt: pr.ArchivedAt, UpdatedAt: pr.UpdatedAt,
	})
	return postgres.RequireAffected("update project", rows, err)
}

func (s *Projects) Delete(ctx context.Context, sc domain.Scope, id string) error {
	p, projectID, err := scopeAndID(sc, id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteProject(ctx, sqlcgen.DeleteProjectParams{ID: projectID, ScopeUser: p.user, ScopeTeams: p.teams})
	return postgres.RequireAffected("delete project", rows, err)
}

func (s *Projects) List(ctx context.Context, sc domain.Scope, f domain.ProjectFilter) ([]*domain.Project, error) {
	p, err := parseScope(sc)
	if err != nil {
		return nil, err
	}
	teamID, err := optionalUUID(f.TeamID)
	if err != nil {
		return nil, err
	}
	collectionID, err := optionalUUID(f.CollectionID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).ListProjects(ctx, sqlcgen.ListProjectsParams{
		ScopeUser: p.user, ScopeTeams: p.teams, Personal: f.Personal, TeamID: teamID,
		CollectionID: collectionID, Archive: int32(f.Archive),
	})
	if err != nil {
		return nil, postgres.MapErr("list projects", err)
	}
	out := make([]*domain.Project, 0, len(rows))
	for _, r := range rows {
		out = append(out, projectToDomain(r))
	}
	return out, nil
}

func (s *Projects) DetachCollection(ctx context.Context, sc domain.Scope, collectionID string) error {
	p, collectionUUID, err := scopeAndID(sc, collectionID)
	if err != nil {
		return err
	}
	err = s.q(ctx).DetachCollection(ctx, sqlcgen.DetachCollectionParams{CollectionID: &collectionUUID, ScopeUser: p.user, ScopeTeams: p.teams})
	return postgres.MapErr("detach collection", err)
}

func (s *Projects) DeleteByUser(ctx context.Context, userID string) error {
	userUUID, err := ids.UUID(userID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete projects by user", s.q(ctx).DeleteProjectsByUser(ctx, userUUID))
}

func (s *Projects) DeleteByTeam(ctx context.Context, teamID string) error {
	teamUUID, err := ids.UUID(teamID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete projects by team", s.q(ctx).DeleteProjectsByTeam(ctx, &teamUUID))
}

// ---- collections ----

type Collections struct{ base }

var _ application.CollectionStore = (*Collections)(nil)

func NewCollections(pool *pgxpool.Pool) *Collections { return &Collections{base{pool}} }

func collectionToDomain(c sqlcgen.Collection) *domain.Collection {
	return &domain.Collection{
		ID: c.ID.String(), UserID: c.UserID.String(), TeamID: idString(c.TeamID),
		Meta: metaToDomain(c.Name, c.Description, c.Icon, c.Color, c.ArchivedAt, c.CreatedAt, c.UpdatedAt),
	}
}

func (s *Collections) Create(ctx context.Context, c *domain.Collection) error {
	userID, err := ids.UUID(c.UserID)
	if err != nil {
		return err
	}
	teamID, err := optionalUUID(c.TeamID)
	if err != nil {
		return err
	}
	row, err := s.q(ctx).CreateCollection(ctx, sqlcgen.CreateCollectionParams{
		UserID: userID, TeamID: teamID, Name: c.Name, Description: c.Description, Icon: c.Icon, Color: c.Color,
		ArchivedAt: c.ArchivedAt, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	})
	if err != nil {
		return postgres.MapErr("create collection", err)
	}
	*c = *collectionToDomain(row)
	return nil
}

func (s *Collections) Get(ctx context.Context, sc domain.Scope, id string) (*domain.Collection, error) {
	p, collectionID, err := scopeAndID(sc, id)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetCollection(ctx, sqlcgen.GetCollectionParams{ID: collectionID, ScopeUser: p.user, ScopeTeams: p.teams})
	if err != nil {
		return nil, postgres.MapErr("get collection", err)
	}
	return collectionToDomain(row), nil
}

func (s *Collections) Update(ctx context.Context, sc domain.Scope, c *domain.Collection) error {
	p, collectionID, err := scopeAndID(sc, c.ID)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).UpdateCollection(ctx, sqlcgen.UpdateCollectionParams{
		ID: collectionID, ScopeUser: p.user, ScopeTeams: p.teams, Name: c.Name, Description: c.Description,
		Icon: c.Icon, Color: c.Color, ArchivedAt: c.ArchivedAt, UpdatedAt: c.UpdatedAt,
	})
	return postgres.RequireAffected("update collection", rows, err)
}

func (s *Collections) Delete(ctx context.Context, sc domain.Scope, id string) error {
	p, collectionID, err := scopeAndID(sc, id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DeleteCollection(ctx, sqlcgen.DeleteCollectionParams{ID: collectionID, ScopeUser: p.user, ScopeTeams: p.teams})
	return postgres.RequireAffected("delete collection", rows, err)
}

func (s *Collections) List(ctx context.Context, sc domain.Scope, f domain.CollectionFilter) ([]*domain.Collection, error) {
	p, err := parseScope(sc)
	if err != nil {
		return nil, err
	}
	teamID, err := optionalUUID(f.TeamID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).ListCollections(ctx, sqlcgen.ListCollectionsParams{
		ScopeUser: p.user, ScopeTeams: p.teams, Personal: f.Personal, TeamID: teamID, Archive: int32(f.Archive),
	})
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

func (s *Collections) DeleteByTeam(ctx context.Context, teamID string) error {
	teamUUID, err := ids.UUID(teamID)
	if err != nil {
		return err
	}
	return postgres.MapErr("delete collections by team", s.q(ctx).DeleteCollectionsByTeam(ctx, &teamUUID))
}
