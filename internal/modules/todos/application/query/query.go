// Package query holds the todos module's read use cases. Query handlers never
// change state and depend only on read ports; project and collection reads
// come back as views enriched with the progress of their todos.
package query

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

type Deps struct {
	Todos       application.TodoReader
	Projects    application.ProjectReader
	Collections application.CollectionReader
	Now         func() time.Time // nil means time.Now
}

// Handlers are all todos-module queries.
type Handlers struct {
	GetTodo         GetTodoHandler
	ListTodos       ListTodosHandler
	GetProject      GetProjectHandler
	ListProjects    ListProjectsHandler
	GetCollection   GetCollectionHandler
	ListCollections ListCollectionsHandler
}

func New(d Deps) Handlers {
	if d.Now == nil {
		d.Now = time.Now
	}
	b := base{d}
	return Handlers{
		GetTodo: GetTodoHandler{b}, ListTodos: ListTodosHandler{b},
		GetProject: GetProjectHandler{b}, ListProjects: ListProjectsHandler{b},
		GetCollection: GetCollectionHandler{b}, ListCollections: ListCollectionsHandler{b},
	}
}

// Now is the clock queries use (e.g. to tell whether a todo is overdue).
func (h Handlers) Now() time.Time { return h.GetTodo.now() }

type base struct{ Deps }

func (b base) now() time.Time { return domain.Timestamp(b.Now()) }

// ---- todos ----

type GetTodo struct {
	UserID, ID string
}

type GetTodoHandler struct{ base }

func (h GetTodoHandler) Handle(ctx context.Context, q GetTodo) (*domain.Todo, error) {
	return h.Todos.Get(ctx, q.UserID, q.ID)
}

// ListTodos selects and orders the user's todos.
type ListTodos struct {
	UserID string
	domain.Query
	// Overdue keeps only open todos past their deadline.
	Overdue bool
	// IncludeArchived also lists todos of archived projects. They are hidden
	// by default unless the query asks for that project explicitly.
	IncludeArchived bool
}

type ListTodosHandler struct{ base }

func (h ListTodosHandler) Handle(ctx context.Context, q ListTodos) ([]*domain.Todo, error) {
	query := q.Query
	switch {
	case query.ProjectID != "":
		if _, err := h.Projects.Get(ctx, q.UserID, query.ProjectID); err != nil {
			return nil, apperrors.UnresolvedAs(err, domain.ErrProjectNotFound)
		}
	case !q.IncludeArchived:
		archived, err := h.Projects.List(ctx, q.UserID, domain.ProjectFilter{Archive: domain.ArchivedOnly})
		if err != nil {
			return nil, err
		}
		for _, p := range archived {
			query.ExcludeProjectIDs = append(query.ExcludeProjectIDs, p.ID)
		}
	}
	if q.Overdue {
		now := h.now()
		query.OverdueAt = &now
	}
	return h.Todos.List(ctx, q.UserID, query)
}

// ---- projects ----

// ProjectView is a project with the progress of its todos.
type ProjectView struct {
	*domain.Project
	Progress domain.Progress
}

type GetProject struct {
	UserID, ID string
}

type GetProjectHandler struct{ base }

func (h GetProjectHandler) Handle(ctx context.Context, q GetProject) (ProjectView, error) {
	p, err := h.Projects.Get(ctx, q.UserID, q.ID)
	if err != nil {
		return ProjectView{}, apperrors.NotFoundAs(err, domain.ErrProjectNotFound)
	}
	views, err := h.withProgress(ctx, q.UserID, []*domain.Project{p})
	if err != nil {
		return ProjectView{}, err
	}
	return views[0], nil
}

type ListProjects struct {
	UserID string
	domain.ProjectFilter
}

type ListProjectsHandler struct{ base }

func (h ListProjectsHandler) Handle(ctx context.Context, q ListProjects) ([]ProjectView, error) {
	if q.CollectionID != "" {
		if _, err := h.Collections.Get(ctx, q.UserID, q.CollectionID); err != nil {
			return nil, apperrors.UnresolvedAs(err, domain.ErrCollectionNotFound)
		}
	}
	projects, err := h.Projects.List(ctx, q.UserID, q.ProjectFilter)
	if err != nil {
		return nil, err
	}
	return h.withProgress(ctx, q.UserID, projects)
}

// withProgress attaches each project's progress (one aggregate read).
func (b base) withProgress(ctx context.Context, userID string, projects []*domain.Project) ([]ProjectView, error) {
	views := make([]ProjectView, 0, len(projects))
	if len(projects) == 0 {
		return views, nil
	}
	progress, err := b.Todos.Progress(ctx, userID, b.now())
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		views = append(views, ProjectView{Project: p, Progress: progress[p.ID]})
	}
	return views, nil
}

// ---- collections ----

// CollectionView is a collection with its active projects and their
// combined progress.
type CollectionView struct {
	*domain.Collection
	Projects []ProjectView
	Progress domain.Progress
}

type GetCollection struct {
	UserID, ID string
}

type GetCollectionHandler struct{ base }

func (h GetCollectionHandler) Handle(ctx context.Context, q GetCollection) (CollectionView, error) {
	c, err := h.Collections.Get(ctx, q.UserID, q.ID)
	if err != nil {
		return CollectionView{}, apperrors.NotFoundAs(err, domain.ErrCollectionNotFound)
	}
	views, err := h.collectionViews(ctx, q.UserID, []*domain.Collection{c})
	if err != nil {
		return CollectionView{}, err
	}
	return views[0], nil
}

type ListCollections struct {
	UserID  string
	Archive domain.ArchiveFilter
}

type ListCollectionsHandler struct{ base }

func (h ListCollectionsHandler) Handle(ctx context.Context, q ListCollections) ([]CollectionView, error) {
	collections, err := h.Collections.List(ctx, q.UserID, q.Archive)
	if err != nil {
		return nil, err
	}
	return h.collectionViews(ctx, q.UserID, collections)
}

func (b base) collectionViews(ctx context.Context, userID string, collections []*domain.Collection) ([]CollectionView, error) {
	views := make([]CollectionView, 0, len(collections))
	if len(collections) == 0 {
		return views, nil
	}
	projects, err := b.Projects.List(ctx, userID, domain.ProjectFilter{Archive: domain.ActiveOnly})
	if err != nil {
		return nil, err
	}
	projectViews, err := b.withProgress(ctx, userID, projects)
	if err != nil {
		return nil, err
	}
	for _, c := range collections {
		v := CollectionView{Collection: c, Projects: []ProjectView{}}
		for _, pv := range projectViews {
			if pv.CollectionID == c.ID {
				v.Projects = append(v.Projects, pv)
				v.Progress.Add(pv.Progress)
			}
		}
		views = append(views, v)
	}
	return views, nil
}
