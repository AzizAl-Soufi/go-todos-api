// Package mcpapi is the todos module's MCP transport: tools for todos,
// projects and collections. Each tool runs the same command or query as the
// matching REST endpoint and returns the same JSON shape.
package mcpapi

import (
	"context"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/transport/httpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/mcpx"
)

const (
	defaultLimit = 50
	maxLimit     = 200
)

type tools struct {
	cmd         command.Handlers
	qry         query.Handlers
	limitCreate func(userID string) error
}

// Register adds the todos, projects and collections tools to s.
// limitCreate guards create_todo with POST /todos's per-user budget (a
// team task can notify the whole team); nil means no limit.
func Register(s *mcp.Server, cmd command.Handlers, qry query.Handlers, limitCreate func(userID string) error) {
	if limitCreate == nil {
		limitCreate = func(string) error { return nil }
	}
	t := tools{cmd: cmd, qry: qry, limitCreate: limitCreate}

	mcpx.Add(s, &mcp.Tool{
		Name: "list_todos",
		Description: "List the caller's todos (personal and in their teams), newest first unless sorted otherwise. " +
			"Filters combine. Todos of archived projects are hidden unless includeArchived is set or the project is asked for. " +
			"Returns at most `limit` todos and says if there were more.",
		Annotations: mcpx.Read("List todos"),
	}, t.listTodos)
	mcpx.Add(s, &mcp.Tool{
		Name:        "get_todo",
		Description: "Get one todo by ID. Use list_todos to find IDs.",
		Annotations: mcpx.Read("Get todo"),
	}, t.getTodo)
	mcpx.Add(s, &mcp.Tool{
		Name: "create_todo",
		Description: "Create a todo, in a project or in the personal inbox (no projectId). " +
			"A todo in a team project belongs to that team; it needs the member role or above there. " +
			"When a team owner or admin adds one, the team's other members get a push notification.",
		Annotations: mcpx.Write("Create todo", false),
	}, t.createTodo)
	mcpx.Add(s, &mcp.Tool{
		Name: "update_todo",
		Description: "Change a todo: only the fields given change. Set status to done to complete it. " +
			"clearDueAt removes the deadline; moveToInbox takes it out of its project.",
		Annotations: mcpx.Write("Update todo", true),
	}, t.updateTodo)
	mcpx.Add(s, &mcp.Tool{
		Name:        "delete_todo",
		Description: "Delete a todo permanently.",
		Annotations: mcpx.Destructive("Delete todo"),
	}, t.deleteTodo)

	mcpx.Add(s, &mcp.Tool{
		Name: "list_projects",
		Description: "List the caller's projects (lists of todos), personal and in their teams, each with its progress. " +
			"Archived projects are left out unless archived is \"true\" or \"all\".",
		Annotations: mcpx.Read("List projects"),
	}, t.listProjects)
	mcpx.Add(s, &mcp.Tool{
		Name:        "get_project",
		Description: "Get one project with its progress (counts per status, overdue, percent done).",
		Annotations: mcpx.Read("Get project"),
	}, t.getProject)
	mcpx.Add(s, &mcp.Tool{
		Name: "create_project",
		Description: "Create a project, personal or in a team (teamId; needs the member role or above). " +
			"It can go in a collection of the same owner.",
		Annotations: mcpx.Write("Create project", false),
	}, t.createProject)
	mcpx.Add(s, &mcp.Tool{
		Name: "update_project",
		Description: "Change a project: only the fields given change. archived true hides it and its todos; " +
			"removeFromCollection takes it out of its collection. A project can't change owner.",
		Annotations: mcpx.Write("Update project", true),
	}, t.updateProject)
	mcpx.Add(s, &mcp.Tool{
		Name:        "delete_project",
		Description: "Delete a project and all its todos. In a team this needs the admin role or above.",
		Annotations: mcpx.Destructive("Delete project and its todos"),
	}, t.deleteProject)

	mcpx.Add(s, &mcp.Tool{
		Name:        "list_collections",
		Description: "List the caller's collections (groups of projects) with their projects and progress.",
		Annotations: mcpx.Read("List collections"),
	}, t.listCollections)
	mcpx.Add(s, &mcp.Tool{
		Name:        "get_collection",
		Description: "Get one collection with its projects and progress.",
		Annotations: mcpx.Read("Get collection"),
	}, t.getCollection)
	mcpx.Add(s, &mcp.Tool{
		Name:        "create_collection",
		Description: "Create a collection to group projects, personal or in a team (teamId; needs the member role or above).",
		Annotations: mcpx.Write("Create collection", false),
	}, t.createCollection)
	mcpx.Add(s, &mcp.Tool{
		Name:        "update_collection",
		Description: "Change a collection: only the fields given change. archived true hides it.",
		Annotations: mcpx.Write("Update collection", true),
	}, t.updateCollection)
	mcpx.Add(s, &mcp.Tool{
		Name:        "delete_collection",
		Description: "Delete a collection. Its projects are kept and leave the collection. In a team this needs the admin role or above.",
		Annotations: mcpx.Destructive("Delete collection"),
	}, t.deleteCollection)
}

// ---- inputs ----

type byID struct {
	ID string `json:"id" jsonschema:"the record's ID"`
}

const timeHelp = "an RFC 3339 timestamp or a YYYY-MM-DD date (midnight UTC)"

type listTodosInput struct {
	Team            string   `json:"team,omitempty" jsonschema:"a team ID, or \"personal\" for personal todos only; omit for all"`
	Project         string   `json:"project,omitempty" jsonschema:"a project ID, or \"inbox\" for todos without a project"`
	Status          []string `json:"status,omitempty" jsonschema:"keep these statuses: pending, in_progress, blocked, done, cancelled"`
	Priority        []string `json:"priority,omitempty" jsonschema:"keep these priorities: none, low, medium, high, urgent"`
	Tag             string   `json:"tag,omitempty" jsonschema:"keep todos with this exact tag"`
	Search          string   `json:"search,omitempty" jsonschema:"case-insensitive text in the title or notes"`
	DueBefore       string   `json:"dueBefore,omitempty" jsonschema:"deadline strictly before this: an RFC 3339 timestamp or a YYYY-MM-DD date"`
	DueAfter        string   `json:"dueAfter,omitempty" jsonschema:"deadline at or after this: an RFC 3339 timestamp or a YYYY-MM-DD date"`
	Overdue         bool     `json:"overdue,omitempty" jsonschema:"only open todos past their deadline"`
	IncludeArchived bool     `json:"includeArchived,omitempty" jsonschema:"also list todos of archived projects"`
	Sort            string   `json:"sort,omitempty" jsonschema:"created (newest first, default), updated, due (soonest first) or priority"`
	Limit           int      `json:"limit,omitempty" jsonschema:"at most this many todos, 1 to 200 (default 50)"`
}

type todoFields struct {
	Title    *string  `json:"title,omitempty" jsonschema:"what to do, up to 500 characters"`
	Notes    *string  `json:"notes,omitempty" jsonschema:"details, up to 10000 characters"`
	Status   *string  `json:"status,omitempty" jsonschema:"pending, in_progress, blocked, done or cancelled"`
	Priority *string  `json:"priority,omitempty" jsonschema:"none, low, medium, high or urgent"`
	Tags     []string `json:"tags,omitempty" jsonschema:"labels; replaces the existing tags"`
	DueAt    *string  `json:"dueAt,omitempty" jsonschema:"the deadline: an RFC 3339 timestamp or a YYYY-MM-DD date (midnight UTC)"`
}

type createTodoInput struct {
	Title     string   `json:"title" jsonschema:"what to do, up to 500 characters"`
	Notes     *string  `json:"notes,omitempty" jsonschema:"details, up to 10000 characters"`
	Status    *string  `json:"status,omitempty" jsonschema:"pending (default), in_progress, blocked, done or cancelled"`
	Priority  *string  `json:"priority,omitempty" jsonschema:"none (default), low, medium, high or urgent"`
	Tags      []string `json:"tags,omitempty" jsonschema:"labels"`
	DueAt     *string  `json:"dueAt,omitempty" jsonschema:"the deadline: an RFC 3339 timestamp or a YYYY-MM-DD date (midnight UTC)"`
	ProjectID string   `json:"projectId,omitempty" jsonschema:"the project to add it to; omit for the personal inbox"`
}

type updateTodoInput struct {
	ID string `json:"id" jsonschema:"the todo's ID"`
	todoFields
	ClearDueAt  bool   `json:"clearDueAt,omitempty" jsonschema:"remove the deadline"`
	ProjectID   string `json:"projectId,omitempty" jsonschema:"move the todo to this project"`
	MoveToInbox bool   `json:"moveToInbox,omitempty" jsonschema:"take the todo out of its project"`
}

type listInput struct {
	Team     string `json:"team,omitempty" jsonschema:"a team ID, or \"personal\" for personal ones only; omit for all"`
	Archived string `json:"archived,omitempty" jsonschema:"false (default: active only), true (archived only) or all"`
}

type listProjectsInput struct {
	listInput
	Collection string `json:"collection,omitempty" jsonschema:"only projects in this collection"`
}

type metaFields struct {
	Description *string `json:"description,omitempty" jsonschema:"up to 1000 characters"`
	Icon        *string `json:"icon,omitempty" jsonschema:"an emoji or short icon name"`
	Color       *string `json:"color,omitempty" jsonschema:"default, gray, brown, orange, yellow, green, blue, purple, pink or red"`
}

type createProjectInput struct {
	Name string `json:"name" jsonschema:"up to 100 characters"`
	metaFields
	TeamID       string `json:"teamId,omitempty" jsonschema:"the team that owns it; omit for a personal project"`
	CollectionID string `json:"collectionId,omitempty" jsonschema:"a collection of the same owner to put it in"`
}

type updateProjectInput struct {
	ID   string  `json:"id" jsonschema:"the project's ID"`
	Name *string `json:"name,omitempty" jsonschema:"up to 100 characters"`
	metaFields
	Archived             *bool  `json:"archived,omitempty" jsonschema:"true archives it, false restores it"`
	CollectionID         string `json:"collectionId,omitempty" jsonschema:"move it to this collection"`
	RemoveFromCollection bool   `json:"removeFromCollection,omitempty" jsonschema:"take it out of its collection"`
}

type createCollectionInput struct {
	Name string `json:"name" jsonschema:"up to 100 characters"`
	metaFields
	TeamID string `json:"teamId,omitempty" jsonschema:"the team that owns it; omit for a personal collection"`
}

type updateCollectionInput struct {
	ID   string  `json:"id" jsonschema:"the collection's ID"`
	Name *string `json:"name,omitempty" jsonschema:"up to 100 characters"`
	metaFields
	Archived *bool `json:"archived,omitempty" jsonschema:"true archives it, false restores it"`
}

// ---- outputs ----

type todoList struct {
	Todos     []httpapi.TodoResponse `json:"todos"`
	Total     int                    `json:"total" jsonschema:"how many todos matched"`
	Truncated bool                   `json:"truncated" jsonschema:"true when more matched than are listed; narrow the filters or raise limit"`
}

type projectList struct {
	Projects []httpapi.ProjectResponse `json:"projects"`
}

type collectionList struct {
	Collections []httpapi.CollectionResponse `json:"collections"`
}

// ---- todos ----

func (t tools) listTodos(ctx context.Context, a access.Actor, in listTodosInput) (todoList, error) {
	q := query.ListTodos{Actor: a, Query: domain.Query{OwnerFilter: owner(in.Team)}}
	switch in.Project {
	case "":
	case "inbox":
		q.Inbox = true
	default:
		q.ProjectID = in.Project
	}
	var err error
	if q.Statuses, err = parseEach(in.Status, domain.ParseStatus); err != nil {
		return todoList{}, err
	}
	if q.Priorities, err = parseEach(in.Priority, domain.ParsePriority); err != nil {
		return todoList{}, err
	}
	if q.DueBefore, err = optionalTime("dueBefore", in.DueBefore); err != nil {
		return todoList{}, err
	}
	if q.DueAfter, err = optionalTime("dueAfter", in.DueAfter); err != nil {
		return todoList{}, err
	}
	if q.Sort, err = domain.ParseSort(in.Sort); err != nil {
		return todoList{}, err
	}
	q.Overdue, q.IncludeArchived = in.Overdue, in.IncludeArchived
	q.Tag, q.Search = strings.TrimSpace(in.Tag), strings.TrimSpace(in.Search)
	limit, err := listLimit(in.Limit)
	if err != nil {
		return todoList{}, err
	}

	todos, err := t.qry.ListTodos.Handle(ctx, q)
	if err != nil {
		return todoList{}, err
	}
	out := todoList{Todos: []httpapi.TodoResponse{}, Total: len(todos), Truncated: len(todos) > limit}
	now := t.qry.Now()
	for _, todo := range todos[:min(limit, len(todos))] {
		out.Todos = append(out.Todos, httpapi.ToTodo(todo, now))
	}
	return out, nil
}

func (t tools) getTodo(ctx context.Context, a access.Actor, in byID) (httpapi.TodoResponse, error) {
	todo, err := t.qry.GetTodo.Handle(ctx, query.GetTodo{Actor: a, ID: in.ID})
	if err != nil {
		return httpapi.TodoResponse{}, err
	}
	return httpapi.ToTodo(todo, t.qry.Now()), nil
}

func (t tools) createTodo(ctx context.Context, a access.Actor, in createTodoInput) (httpapi.TodoResponse, error) {
	if err := t.limitCreate(a.UserID); err != nil {
		return httpapi.TodoResponse{}, err
	}
	c, err := changes(todoFields{Title: &in.Title, Notes: in.Notes, Status: in.Status, Priority: in.Priority, Tags: in.Tags, DueAt: in.DueAt})
	if err != nil {
		return httpapi.TodoResponse{}, err
	}
	if in.ProjectID != "" {
		c.ProjectID = domain.Set(in.ProjectID)
	}
	id, err := t.cmd.CreateTodo.Handle(ctx, command.CreateTodo{Actor: a, Changes: c})
	if err != nil {
		return httpapi.TodoResponse{}, err
	}
	return t.getTodo(ctx, a, byID{ID: id})
}

func (t tools) updateTodo(ctx context.Context, a access.Actor, in updateTodoInput) (httpapi.TodoResponse, error) {
	c, err := changes(in.todoFields)
	if err != nil {
		return httpapi.TodoResponse{}, err
	}
	switch {
	case in.ClearDueAt && in.DueAt != nil, in.MoveToInbox && in.ProjectID != "":
		return httpapi.TodoResponse{}, conflicting
	case in.ClearDueAt:
		c.DueAt = domain.Clear[time.Time]()
	}
	switch {
	case in.MoveToInbox:
		c.ProjectID = domain.Clear[string]()
	case in.ProjectID != "":
		c.ProjectID = domain.Set(in.ProjectID)
	}
	if err := t.cmd.UpdateTodo.Handle(ctx, command.UpdateTodo{Actor: a, ID: in.ID, Changes: c}); err != nil {
		return httpapi.TodoResponse{}, err
	}
	return t.getTodo(ctx, a, byID{ID: in.ID})
}

func (t tools) deleteTodo(ctx context.Context, a access.Actor, in byID) (mcpx.Deleted, error) {
	if err := t.cmd.DeleteTodo.Handle(ctx, command.DeleteTodo{Actor: a, ID: in.ID}); err != nil {
		return mcpx.Deleted{}, err
	}
	return mcpx.Deleted{ID: in.ID, Deleted: true}, nil
}

// ---- projects ----

func (t tools) listProjects(ctx context.Context, a access.Actor, in listProjectsInput) (projectList, error) {
	archive, err := archiveFilter(in.Archived)
	if err != nil {
		return projectList{}, err
	}
	views, err := t.qry.ListProjects.Handle(ctx, query.ListProjects{
		Actor:         a,
		ProjectFilter: domain.ProjectFilter{OwnerFilter: owner(in.Team), CollectionID: in.Collection, Archive: archive},
	})
	if err != nil {
		return projectList{}, err
	}
	out := projectList{Projects: make([]httpapi.ProjectResponse, 0, len(views))}
	for _, v := range views {
		out.Projects = append(out.Projects, httpapi.ToProject(v))
	}
	return out, nil
}

func (t tools) getProject(ctx context.Context, a access.Actor, in byID) (httpapi.ProjectResponse, error) {
	v, err := t.qry.GetProject.Handle(ctx, query.GetProject{Actor: a, ID: in.ID})
	if err != nil {
		return httpapi.ProjectResponse{}, err
	}
	return httpapi.ToProject(v), nil
}

func (t tools) createProject(ctx context.Context, a access.Actor, in createProjectInput) (httpapi.ProjectResponse, error) {
	meta := in.metaFields.changes(&in.Name, nil)
	id, err := t.cmd.CreateProject.Handle(ctx, command.CreateProject{Actor: a, TeamID: in.TeamID, Meta: meta, CollectionID: in.CollectionID})
	if err != nil {
		return httpapi.ProjectResponse{}, err
	}
	return t.getProject(ctx, a, byID{ID: id})
}

func (t tools) updateProject(ctx context.Context, a access.Actor, in updateProjectInput) (httpapi.ProjectResponse, error) {
	var collection domain.Patch[string]
	switch {
	case in.RemoveFromCollection && in.CollectionID != "":
		return httpapi.ProjectResponse{}, conflicting
	case in.RemoveFromCollection:
		collection = domain.Clear[string]()
	case in.CollectionID != "":
		collection = domain.Set(in.CollectionID)
	}
	meta := in.metaFields.changes(in.Name, in.Archived)
	if meta.Empty() && !collection.Set {
		return httpapi.ProjectResponse{}, nothingToChange
	}
	err := t.cmd.UpdateProject.Handle(ctx, command.UpdateProject{Actor: a, ID: in.ID, Meta: meta, CollectionID: collection})
	if err != nil {
		return httpapi.ProjectResponse{}, err
	}
	return t.getProject(ctx, a, byID{ID: in.ID})
}

func (t tools) deleteProject(ctx context.Context, a access.Actor, in byID) (mcpx.Deleted, error) {
	if err := t.cmd.DeleteProject.Handle(ctx, command.DeleteProject{Actor: a, ID: in.ID}); err != nil {
		return mcpx.Deleted{}, err
	}
	return mcpx.Deleted{ID: in.ID, Deleted: true}, nil
}

// ---- collections ----

func (t tools) listCollections(ctx context.Context, a access.Actor, in listInput) (collectionList, error) {
	archive, err := archiveFilter(in.Archived)
	if err != nil {
		return collectionList{}, err
	}
	views, err := t.qry.ListCollections.Handle(ctx, query.ListCollections{
		Actor:            a,
		CollectionFilter: domain.CollectionFilter{OwnerFilter: owner(in.Team), Archive: archive},
	})
	if err != nil {
		return collectionList{}, err
	}
	out := collectionList{Collections: make([]httpapi.CollectionResponse, 0, len(views))}
	for _, v := range views {
		out.Collections = append(out.Collections, httpapi.ToCollection(v))
	}
	return out, nil
}

func (t tools) getCollection(ctx context.Context, a access.Actor, in byID) (httpapi.CollectionResponse, error) {
	v, err := t.qry.GetCollection.Handle(ctx, query.GetCollection{Actor: a, ID: in.ID})
	if err != nil {
		return httpapi.CollectionResponse{}, err
	}
	return httpapi.ToCollection(v), nil
}

func (t tools) createCollection(ctx context.Context, a access.Actor, in createCollectionInput) (httpapi.CollectionResponse, error) {
	meta := in.metaFields.changes(&in.Name, nil)
	id, err := t.cmd.CreateCollection.Handle(ctx, command.CreateCollection{Actor: a, TeamID: in.TeamID, Meta: meta})
	if err != nil {
		return httpapi.CollectionResponse{}, err
	}
	return t.getCollection(ctx, a, byID{ID: id})
}

func (t tools) updateCollection(ctx context.Context, a access.Actor, in updateCollectionInput) (httpapi.CollectionResponse, error) {
	meta := in.metaFields.changes(in.Name, in.Archived)
	if meta.Empty() {
		return httpapi.CollectionResponse{}, nothingToChange
	}
	if err := t.cmd.UpdateCollection.Handle(ctx, command.UpdateCollection{Actor: a, ID: in.ID, Meta: meta}); err != nil {
		return httpapi.CollectionResponse{}, err
	}
	return t.getCollection(ctx, a, byID{ID: in.ID})
}

func (t tools) deleteCollection(ctx context.Context, a access.Actor, in byID) (mcpx.Deleted, error) {
	if err := t.cmd.DeleteCollection.Handle(ctx, command.DeleteCollection{Actor: a, ID: in.ID}); err != nil {
		return mcpx.Deleted{}, err
	}
	return mcpx.Deleted{ID: in.ID, Deleted: true}, nil
}

// ---- helpers ----

var (
	conflicting     = apperrors.Validation("CONFLICTING_FIELDS", "two of the given fields contradict each other")
	nothingToChange = apperrors.Validation("NOTHING_TO_CHANGE", "give at least one field to change")
)

func owner(team string) domain.OwnerFilter {
	switch team {
	case "":
		return domain.OwnerFilter{}
	case "personal":
		return domain.OwnerFilter{Personal: true}
	default:
		return domain.OwnerFilter{TeamID: team}
	}
}

func archiveFilter(s string) (domain.ArchiveFilter, error) {
	switch s {
	case "", "false":
		return domain.ActiveOnly, nil
	case "true":
		return domain.ArchivedOnly, nil
	case "all":
		return domain.AnyArchiveState, nil
	}
	return 0, apperrors.Validation("INVALID_FIELD", "archived must be true, false or all")
}

func listLimit(n int) (int, error) {
	switch {
	case n == 0:
		return defaultLimit, nil
	case n < 1 || n > maxLimit:
		return 0, apperrors.Validation("INVALID_FIELD", "limit must be between 1 and 200")
	}
	return n, nil
}

func parseEach[T any](values []string, parse func(string) (T, error)) ([]T, error) {
	var out []T
	for _, v := range values {
		parsed, err := parse(strings.TrimSpace(v))
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func parseTime(field, s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, apperrors.Validation("INVALID_FIELD", field+" must be "+timeHelp)
	}
	return t, nil
}

func optionalTime(field, s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := parseTime(field, s)
	return &t, err
}

// changes turns the todo fields given into domain changes.
func changes(f todoFields) (domain.Changes, error) {
	c := domain.Changes{Title: f.Title, Notes: f.Notes}
	if f.Status != nil {
		s, err := domain.ParseStatus(*f.Status)
		if err != nil {
			return c, err
		}
		c.Status = &s
	}
	if f.Priority != nil {
		p, err := domain.ParsePriority(*f.Priority)
		if err != nil {
			return c, err
		}
		c.Priority = &p
	}
	if f.Tags != nil {
		tags := f.Tags
		c.Tags = &tags
	}
	if f.DueAt != nil {
		due, err := parseTime("dueAt", *f.DueAt)
		if err != nil {
			return c, err
		}
		c.DueAt = domain.Set(due)
	}
	return c, nil
}

func (m metaFields) changes(name *string, archived *bool) domain.MetaChanges {
	return domain.MetaChanges{Name: name, Description: m.Description, Icon: m.Icon, Color: m.Color, Archived: archived}
}
