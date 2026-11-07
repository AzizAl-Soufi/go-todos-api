package httpapi

import (
	"net/http"
	"net/url"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
)

// ---- response shapes ----

type progressResponse struct {
	Total       int `json:"total"`
	Pending     int `json:"pending"`
	InProgress  int `json:"inProgress"`
	Blocked     int `json:"blocked"`
	Done        int `json:"done"`
	Cancelled   int `json:"cancelled"`
	Overdue     int `json:"overdue"`
	PercentDone int `json:"percentDone"`
}

func toProgress(p domain.Progress) progressResponse {
	return progressResponse{
		Total: p.Total, Overdue: p.Overdue, PercentDone: p.PercentDone(),
		Pending: p.ByStatus[domain.StatusPending], InProgress: p.ByStatus[domain.StatusInProgress],
		Blocked: p.ByStatus[domain.StatusBlocked], Done: p.ByStatus[domain.StatusDone],
		Cancelled: p.ByStatus[domain.StatusCancelled],
	}
}

// metaResponse is what projects and collections share.
type metaResponse struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Icon        string     `json:"icon"`
	Color       string     `json:"color"`
	Archived    bool       `json:"archived"`
	ArchivedAt  *time.Time `json:"archivedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

func toMeta(m domain.Meta) metaResponse {
	return metaResponse{
		Name: m.Name, Description: m.Description, Icon: m.Icon, Color: m.Color,
		Archived: m.Archived(), ArchivedAt: m.ArchivedAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

type projectResponse struct {
	ID           string  `json:"id"`
	CollectionID *string `json:"collectionId"`
	metaResponse
	Progress progressResponse `json:"progress"`
}

func toProject(v query.ProjectView) projectResponse {
	return projectResponse{ID: v.ID, CollectionID: optionalID(v.CollectionID), metaResponse: toMeta(v.Meta), Progress: toProgress(v.Progress)}
}

type collectionResponse struct {
	ID string `json:"id"`
	metaResponse
	Progress progressResponse  `json:"progress"`
	Projects []projectResponse `json:"projects"`
}

func toCollection(v query.CollectionView) collectionResponse {
	projects := make([]projectResponse, 0, len(v.Projects))
	for _, p := range v.Projects {
		projects = append(projects, toProject(p))
	}
	return collectionResponse{ID: v.ID, metaResponse: toMeta(v.Meta), Progress: toProgress(v.Progress), Projects: projects}
}

// ---- requests ----

// decodeMeta reads the fields projects and collections share, plus a
// project's collectionId (collections ignore it).
func decodeMeta(r *http.Request, requireFields bool) (domain.MetaChanges, domain.Patch[string], error) {
	b, err := decodeBody(r, requireFields)
	if err != nil {
		return domain.MetaChanges{}, domain.Patch[string]{}, err
	}
	var c domain.MetaChanges
	var collection domain.Patch[string]
	var errs [6]error
	c.Name, errs[0] = b.str("name", domain.ErrInvalidName)
	c.Description, errs[1] = b.str("description", nil)
	c.Icon, errs[2] = b.str("icon", nil)
	c.Color, errs[3] = b.str("color", nil)
	c.Archived, errs[4] = b.boolean("archived", nil)
	collection, errs[5] = b.id("collectionId")
	return c, collection, firstErr(errs[:]...)
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

func (h handler) readProject(r *http.Request, status int, userID, id string) (int, any, error) {
	v, err := h.qry.GetProject.Handle(r.Context(), query.GetProject{UserID: userID, ID: id})
	return respond(status, v, err, toProject)
}

func (h handler) createProject(r *http.Request, userID string) (int, any, error) {
	meta, collection, err := decodeMeta(r, false)
	if err != nil {
		return 0, nil, err
	}
	collectionID, _ := collection.Get()
	id, err := h.cmd.CreateProject.Handle(r.Context(), command.CreateProject{UserID: userID, Meta: meta, CollectionID: collectionID})
	if err != nil {
		return 0, nil, err
	}
	return h.readProject(r, http.StatusCreated, userID, id)
}

func (h handler) getProject(r *http.Request, userID string) (int, any, error) {
	return h.readProject(r, http.StatusOK, userID, r.PathValue("id"))
}

func (h handler) listProjects(r *http.Request, userID string) (int, any, error) {
	archive, err := parseArchiveFilter(r.URL.Query())
	if err != nil {
		return 0, nil, err
	}
	views, err := h.qry.ListProjects.Handle(r.Context(), query.ListProjects{
		UserID: userID, ProjectFilter: domain.ProjectFilter{CollectionID: r.URL.Query().Get("collection"), Archive: archive},
	})
	return respondList(views, err, toProject)
}

func (h handler) updateProject(r *http.Request, userID string) (int, any, error) {
	meta, collection, err := decodeMeta(r, true)
	if err != nil {
		return 0, nil, err
	}
	id := r.PathValue("id")
	if err := h.cmd.UpdateProject.Handle(r.Context(), command.UpdateProject{UserID: userID, ID: id, Meta: meta, CollectionID: collection}); err != nil {
		return 0, nil, err
	}
	return h.readProject(r, http.StatusOK, userID, id)
}

func (h handler) deleteProject(r *http.Request, userID string) (int, any, error) {
	return http.StatusNoContent, nil, h.cmd.DeleteProject.Handle(r.Context(), command.DeleteProject{UserID: userID, ID: r.PathValue("id")})
}

// ---- collections ----

func (h handler) readCollection(r *http.Request, status int, userID, id string) (int, any, error) {
	v, err := h.qry.GetCollection.Handle(r.Context(), query.GetCollection{UserID: userID, ID: id})
	return respond(status, v, err, toCollection)
}

func (h handler) createCollection(r *http.Request, userID string) (int, any, error) {
	meta, _, err := decodeMeta(r, false)
	if err != nil {
		return 0, nil, err
	}
	id, err := h.cmd.CreateCollection.Handle(r.Context(), command.CreateCollection{UserID: userID, Meta: meta})
	if err != nil {
		return 0, nil, err
	}
	return h.readCollection(r, http.StatusCreated, userID, id)
}

func (h handler) getCollection(r *http.Request, userID string) (int, any, error) {
	return h.readCollection(r, http.StatusOK, userID, r.PathValue("id"))
}

func (h handler) listCollections(r *http.Request, userID string) (int, any, error) {
	archive, err := parseArchiveFilter(r.URL.Query())
	if err != nil {
		return 0, nil, err
	}
	views, err := h.qry.ListCollections.Handle(r.Context(), query.ListCollections{UserID: userID, Archive: archive})
	return respondList(views, err, toCollection)
}

func (h handler) updateCollection(r *http.Request, userID string) (int, any, error) {
	meta, _, err := decodeMeta(r, true)
	if err != nil {
		return 0, nil, err
	}
	id := r.PathValue("id")
	if err := h.cmd.UpdateCollection.Handle(r.Context(), command.UpdateCollection{UserID: userID, ID: id, Meta: meta}); err != nil {
		return 0, nil, err
	}
	return h.readCollection(r, http.StatusOK, userID, id)
}

func (h handler) deleteCollection(r *http.Request, userID string) (int, any, error) {
	return http.StatusNoContent, nil, h.cmd.DeleteCollection.Handle(r.Context(), command.DeleteCollection{UserID: userID, ID: r.PathValue("id")})
}
