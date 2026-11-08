package httpapi

import (
	"net/http"
	"net/url"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
)

// ---- response shapes ----

type ProgressResponse struct {
	Total       int `json:"total"`
	Pending     int `json:"pending"`
	InProgress  int `json:"inProgress"`
	Blocked     int `json:"blocked"`
	Done        int `json:"done"`
	Cancelled   int `json:"cancelled"`
	Overdue     int `json:"overdue"`
	PercentDone int `json:"percentDone"`
}

func ToProgress(p domain.Progress) ProgressResponse {
	return ProgressResponse{
		Total: p.Total, Overdue: p.Overdue, PercentDone: p.PercentDone(),
		Pending: p.ByStatus[domain.StatusPending], InProgress: p.ByStatus[domain.StatusInProgress],
		Blocked: p.ByStatus[domain.StatusBlocked], Done: p.ByStatus[domain.StatusDone],
		Cancelled: p.ByStatus[domain.StatusCancelled],
	}
}

// MetaResponse is what projects and collections share.
type MetaResponse struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Icon        string     `json:"icon"`
	Color       string     `json:"color"`
	Archived    bool       `json:"archived"`
	ArchivedAt  *time.Time `json:"archivedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

func ToMeta(m domain.Meta) MetaResponse {
	return MetaResponse{
		Name: m.Name, Description: m.Description, Icon: m.Icon, Color: m.Color,
		Archived: m.Archived(), ArchivedAt: m.ArchivedAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

type ProjectResponse struct {
	ID           string  `json:"id"`
	UserID       string  `json:"userId"`
	TeamID       *string `json:"teamId"`
	CollectionID *string `json:"collectionId"`
	MetaResponse
	Progress ProgressResponse `json:"progress"`
}

func ToProject(v query.ProjectView) ProjectResponse {
	return ProjectResponse{
		ID: v.ID, UserID: v.UserID, TeamID: optionalID(v.TeamID), CollectionID: optionalID(v.CollectionID),
		MetaResponse: ToMeta(v.Meta), Progress: ToProgress(v.Progress),
	}
}

type CollectionResponse struct {
	ID     string  `json:"id"`
	UserID string  `json:"userId"`
	TeamID *string `json:"teamId"`
	MetaResponse
	Progress ProgressResponse  `json:"progress"`
	Projects []ProjectResponse `json:"projects"`
}

func ToCollection(v query.CollectionView) CollectionResponse {
	projects := make([]ProjectResponse, 0, len(v.Projects))
	for _, p := range v.Projects {
		projects = append(projects, ToProject(p))
	}
	return CollectionResponse{
		ID: v.ID, UserID: v.UserID, TeamID: optionalID(v.TeamID),
		MetaResponse: ToMeta(v.Meta), Progress: ToProgress(v.Progress), Projects: projects,
	}
}

// ---- requests ----

// metaRequest is the body of a project or collection create or update:
// the shared fields, a project's collectionId and the team (set on create
// only; collections ignore collectionId).
type metaRequest struct {
	meta       domain.MetaChanges
	collection domain.Patch[string]
	team       domain.Patch[string]
}

func decodeMeta(r *http.Request, update bool) (metaRequest, error) {
	b, err := decodeBody(r, update)
	if err != nil {
		return metaRequest{}, err
	}
	var m metaRequest
	var errs [7]error
	m.meta.Name, errs[0] = b.str("name", domain.ErrInvalidName)
	m.meta.Description, errs[1] = b.str("description", nil)
	m.meta.Icon, errs[2] = b.str("icon", nil)
	m.meta.Color, errs[3] = b.str("color", nil)
	m.meta.Archived, errs[4] = b.boolean("archived", nil)
	m.collection, errs[5] = b.id("collectionId")
	m.team, errs[6] = b.id("teamId")
	if err := firstErr(errs[:]...); err != nil {
		return metaRequest{}, err
	}
	if update && m.team.Set {
		return metaRequest{}, domain.ErrTeamFixed
	}
	return m, nil
}

func parseArchiveFilter(v url.Values) (domain.ArchiveFilter, error) {
	switch v.Get("archived") {
	case "", "false":
		return domain.ActiveOnly, nil
	case "true":
		return domain.ArchivedOnly, nil
	case "all":
		return domain.AnyArchiveState, nil
	}
	return 0, invalidQuery("archived", "true, false or all")
}

// ---- projects ----

func (h handler) readProject(r *http.Request, status int, a access.Actor, id string) (int, any, error) {
	v, err := h.qry.GetProject.Handle(r.Context(), query.GetProject{Actor: a, ID: id})
	return respond(status, v, err, ToProject)
}

func (h handler) createProject(r *http.Request, a access.Actor) (int, any, error) {
	m, err := decodeMeta(r, false)
	if err != nil {
		return 0, nil, err
	}
	collectionID, _ := m.collection.Get()
	teamID, _ := m.team.Get()
	id, err := h.cmd.CreateProject.Handle(r.Context(), command.CreateProject{Actor: a, TeamID: teamID, Meta: m.meta, CollectionID: collectionID})
	if err != nil {
		return 0, nil, err
	}
	return h.readProject(r, http.StatusCreated, a, id)
}

func (h handler) getProject(r *http.Request, a access.Actor) (int, any, error) {
	return h.readProject(r, http.StatusOK, a, r.PathValue("id"))
}

func (h handler) listProjects(r *http.Request, a access.Actor) (int, any, error) {
	archive, err := parseArchiveFilter(r.URL.Query())
	if err != nil {
		return 0, nil, err
	}
	v := r.URL.Query()
	views, err := h.qry.ListProjects.Handle(r.Context(), query.ListProjects{
		Actor: a, ProjectFilter: domain.ProjectFilter{OwnerFilter: parseOwner(v), CollectionID: v.Get("collection"), Archive: archive},
	})
	return respondList(views, err, ToProject)
}

func (h handler) updateProject(r *http.Request, a access.Actor) (int, any, error) {
	m, err := decodeMeta(r, true)
	if err != nil {
		return 0, nil, err
	}
	id := r.PathValue("id")
	if err := h.cmd.UpdateProject.Handle(r.Context(), command.UpdateProject{Actor: a, ID: id, Meta: m.meta, CollectionID: m.collection}); err != nil {
		return 0, nil, err
	}
	return h.readProject(r, http.StatusOK, a, id)
}

func (h handler) deleteProject(r *http.Request, a access.Actor) (int, any, error) {
	return http.StatusNoContent, nil, h.cmd.DeleteProject.Handle(r.Context(), command.DeleteProject{Actor: a, ID: r.PathValue("id")})
}

// ---- collections ----

func (h handler) readCollection(r *http.Request, status int, a access.Actor, id string) (int, any, error) {
	v, err := h.qry.GetCollection.Handle(r.Context(), query.GetCollection{Actor: a, ID: id})
	return respond(status, v, err, ToCollection)
}

func (h handler) createCollection(r *http.Request, a access.Actor) (int, any, error) {
	m, err := decodeMeta(r, false)
	if err != nil {
		return 0, nil, err
	}
	teamID, _ := m.team.Get()
	id, err := h.cmd.CreateCollection.Handle(r.Context(), command.CreateCollection{Actor: a, TeamID: teamID, Meta: m.meta})
	if err != nil {
		return 0, nil, err
	}
	return h.readCollection(r, http.StatusCreated, a, id)
}

func (h handler) getCollection(r *http.Request, a access.Actor) (int, any, error) {
	return h.readCollection(r, http.StatusOK, a, r.PathValue("id"))
}

func (h handler) listCollections(r *http.Request, a access.Actor) (int, any, error) {
	archive, err := parseArchiveFilter(r.URL.Query())
	if err != nil {
		return 0, nil, err
	}
	views, err := h.qry.ListCollections.Handle(r.Context(), query.ListCollections{
		Actor: a, CollectionFilter: domain.CollectionFilter{OwnerFilter: parseOwner(r.URL.Query()), Archive: archive},
	})
	return respondList(views, err, ToCollection)
}

func (h handler) updateCollection(r *http.Request, a access.Actor) (int, any, error) {
	m, err := decodeMeta(r, true)
	if err != nil {
		return 0, nil, err
	}
	id := r.PathValue("id")
	if err := h.cmd.UpdateCollection.Handle(r.Context(), command.UpdateCollection{Actor: a, ID: id, Meta: m.meta}); err != nil {
		return 0, nil, err
	}
	return h.readCollection(r, http.StatusOK, a, id)
}

func (h handler) deleteCollection(r *http.Request, a access.Actor) (int, any, error) {
	return http.StatusNoContent, nil, h.cmd.DeleteCollection.Handle(r.Context(), command.DeleteCollection{Actor: a, ID: r.PathValue("id")})
}
