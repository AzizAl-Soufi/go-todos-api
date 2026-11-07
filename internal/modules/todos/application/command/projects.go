package command

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// requireCollectionOf checks that a project of teamID may be filed under the
// collection: it must be in scope and belong to the same team.
func (b base) requireCollectionOf(ctx context.Context, a access.Actor, teamID string, collectionID domain.Patch[string]) error {
	id, ok := collectionID.Get()
	if !ok {
		return nil
	}
	c, err := b.collection(ctx, a, id)
	if err != nil {
		return err
	}
	if c.TeamID != teamID {
		return domain.ErrTeamMismatch
	}
	return nil
}

func metaDetails(m domain.Meta, changes domain.MetaChanges) map[string]string {
	d := map[string]string{"name": m.Name}
	if f := changes.Fields(); len(f) > 0 {
		d["fields"] = fields(f)
	}
	return d
}

// ---- projects ----

// CreateProject adds a project (a list of todos), personal or for a team,
// optionally in a collection of the same team.
type CreateProject struct {
	Actor        access.Actor
	TeamID       string             // "" for a personal project
	Meta         domain.MetaChanges // the fields to set; Name is required
	CollectionID string             // "" for none
}

type CreateProjectHandler struct{ base }

func (h CreateProjectHandler) Handle(ctx context.Context, cmd CreateProject) (id string, err error) {
	if err := authorize(cmd.Actor, cmd.TeamID, access.Role.CanEdit); err != nil {
		return "", err
	}
	collection := domain.Patch[string]{Set: cmd.CollectionID != "", Value: &cmd.CollectionID}
	if err := h.requireCollectionOf(ctx, cmd.Actor, cmd.TeamID, collection); err != nil {
		return "", err
	}
	meta, err := domain.NewMeta(cmd.Meta, h.now())
	if err != nil {
		return "", err
	}
	p := &domain.Project{UserID: cmd.Actor.UserID, TeamID: cmd.TeamID, CollectionID: cmd.CollectionID, Meta: meta}
	err = h.commit(ctx,
		func(ctx context.Context) error { return h.Projects.Create(ctx, p) },
		func() application.AuditEntry {
			return event(cmd.Actor, p.TeamID, "project.created", "project", p.ID, metaDetails(p.Meta, domain.MetaChanges{}))
		})
	return p.ID, err
}

// UpdateProject renames, restyles, archives or moves a project between
// collections (of its own team).
type UpdateProject struct {
	Actor access.Actor
	ID    string
	Meta  domain.MetaChanges
	// CollectionID files the project under a collection; clearing it takes
	// the project out of its collection.
	CollectionID domain.Patch[string]
}

type UpdateProjectHandler struct{ base }

func (h UpdateProjectHandler) Handle(ctx context.Context, cmd UpdateProject) error {
	scope := application.ScopeOf(cmd.Actor)
	p, err := h.Projects.Get(ctx, scope, cmd.ID)
	if err != nil {
		return apperrors.NotFoundAs(err, domain.ErrProjectNotFound)
	}
	if err := authorize(cmd.Actor, p.TeamID, access.Role.CanEdit); err != nil {
		return err
	}
	if cmd.Meta.Empty() && !cmd.CollectionID.Set {
		return nil
	}
	if err := h.requireCollectionOf(ctx, cmd.Actor, p.TeamID, cmd.CollectionID); err != nil {
		return err
	}
	if err := p.Meta.Apply(cmd.Meta, h.now()); err != nil {
		return err
	}
	details := metaDetails(p.Meta, cmd.Meta)
	if cmd.CollectionID.Set {
		p.CollectionID, _ = cmd.CollectionID.Get()
		p.UpdatedAt = h.now()
		details["fields"] = fields(append(cmd.Meta.Fields(), "collectionId"))
	}
	return h.commit(ctx,
		func(ctx context.Context) error { return h.Projects.Update(ctx, scope, p) },
		func() application.AuditEntry {
			return event(cmd.Actor, p.TeamID, "project.updated", "project", p.ID, details)
		})
}

// DeleteProject deletes the project together with its todos, atomically.
// Team projects can only be deleted by owners and admins.
type DeleteProject struct {
	Actor access.Actor
	ID    string
}

type DeleteProjectHandler struct{ base }

func (h DeleteProjectHandler) Handle(ctx context.Context, cmd DeleteProject) error {
	scope := application.ScopeOf(cmd.Actor)
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := h.Projects.Get(ctx, scope, cmd.ID)
		if err != nil {
			return apperrors.NotFoundAs(err, domain.ErrProjectNotFound)
		}
		if err := authorize(cmd.Actor, p.TeamID, access.Role.CanManage); err != nil {
			return err
		}
		return h.commit(ctx,
			func(ctx context.Context) error {
				if err := h.Todos.DeleteByProject(ctx, scope, p.ID); err != nil {
					return err
				}
				return apperrors.NotFoundAs(h.Projects.Delete(ctx, scope, p.ID), domain.ErrProjectNotFound)
			},
			func() application.AuditEntry {
				return event(cmd.Actor, p.TeamID, "project.deleted", "project", p.ID, map[string]string{"name": p.Name})
			})
	})
}

// ---- collections ----

// CreateCollection adds a collection to group projects, personal or for a
// team.
type CreateCollection struct {
	Actor  access.Actor
	TeamID string             // "" for a personal collection
	Meta   domain.MetaChanges // the fields to set; Name is required
}

type CreateCollectionHandler struct{ base }

func (h CreateCollectionHandler) Handle(ctx context.Context, cmd CreateCollection) (id string, err error) {
	if err := authorize(cmd.Actor, cmd.TeamID, access.Role.CanEdit); err != nil {
		return "", err
	}
	meta, err := domain.NewMeta(cmd.Meta, h.now())
	if err != nil {
		return "", err
	}
	c := &domain.Collection{UserID: cmd.Actor.UserID, TeamID: cmd.TeamID, Meta: meta}
	err = h.commit(ctx,
		func(ctx context.Context) error { return h.Collections.Create(ctx, c) },
		func() application.AuditEntry {
			return event(cmd.Actor, c.TeamID, "collection.created", "collection", c.ID, metaDetails(c.Meta, domain.MetaChanges{}))
		})
	return c.ID, err
}

// UpdateCollection renames, restyles or archives a collection.
type UpdateCollection struct {
	Actor access.Actor
	ID    string
	Meta  domain.MetaChanges
}

type UpdateCollectionHandler struct{ base }

func (h UpdateCollectionHandler) Handle(ctx context.Context, cmd UpdateCollection) error {
	scope := application.ScopeOf(cmd.Actor)
	c, err := h.Collections.Get(ctx, scope, cmd.ID)
	if err != nil {
		return apperrors.NotFoundAs(err, domain.ErrCollectionNotFound)
	}
	if err := authorize(cmd.Actor, c.TeamID, access.Role.CanEdit); err != nil || cmd.Meta.Empty() {
		return err
	}
	if err := c.Meta.Apply(cmd.Meta, h.now()); err != nil {
		return err
	}
	return h.commit(ctx,
		func(ctx context.Context) error { return h.Collections.Update(ctx, scope, c) },
		func() application.AuditEntry {
			return event(cmd.Actor, c.TeamID, "collection.updated", "collection", c.ID, metaDetails(c.Meta, cmd.Meta))
		})
}

// DeleteCollection deletes the collection only: its projects (and their
// todos) are kept and simply no longer filed under it. Team collections can
// only be deleted by owners and admins.
type DeleteCollection struct {
	Actor access.Actor
	ID    string
}

type DeleteCollectionHandler struct{ base }

func (h DeleteCollectionHandler) Handle(ctx context.Context, cmd DeleteCollection) error {
	scope := application.ScopeOf(cmd.Actor)
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		c, err := h.Collections.Get(ctx, scope, cmd.ID)
		if err != nil {
			return apperrors.NotFoundAs(err, domain.ErrCollectionNotFound)
		}
		if err := authorize(cmd.Actor, c.TeamID, access.Role.CanManage); err != nil {
			return err
		}
		return h.commit(ctx,
			func(ctx context.Context) error {
				if err := h.Projects.DetachCollection(ctx, scope, c.ID); err != nil {
					return err
				}
				return apperrors.NotFoundAs(h.Collections.Delete(ctx, scope, c.ID), domain.ErrCollectionNotFound)
			},
			func() application.AuditEntry {
				return event(cmd.Actor, c.TeamID, "collection.deleted", "collection", c.ID, map[string]string{"name": c.Name})
			})
	})
}
