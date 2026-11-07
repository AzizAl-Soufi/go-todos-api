package command

import (
	"context"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// ---- projects ----

// CreateProject adds a project (a list of todos), optionally in a collection.
type CreateProject struct {
	UserID       string
	Meta         domain.MetaChanges // the fields to set; Name is required
	CollectionID string             // "" for none
}

type CreateProjectHandler struct{ base }

func (h CreateProjectHandler) Handle(ctx context.Context, cmd CreateProject) (id string, err error) {
	if cmd.CollectionID != "" {
		if err := h.requireCollection(ctx, cmd.UserID, cmd.CollectionID); err != nil {
			return "", err
		}
	}
	meta, err := domain.NewMeta(cmd.Meta, h.now())
	if err != nil {
		return "", err
	}
	p := &domain.Project{UserID: cmd.UserID, CollectionID: cmd.CollectionID, Meta: meta}
	if err := h.Projects.Create(ctx, p); err != nil {
		return "", err
	}
	return p.ID, nil
}

// UpdateProject renames, restyles, archives or moves a project.
type UpdateProject struct {
	UserID, ID string
	Meta       domain.MetaChanges
	// CollectionID files the project under a collection; clearing it takes
	// the project out of its collection.
	CollectionID domain.Patch[string]
}

type UpdateProjectHandler struct{ base }

func (h UpdateProjectHandler) Handle(ctx context.Context, cmd UpdateProject) error {
	p, err := h.Projects.Get(ctx, cmd.UserID, cmd.ID)
	if err != nil {
		return apperrors.NotFoundAs(err, domain.ErrProjectNotFound)
	}
	if cmd.Meta.Empty() && !cmd.CollectionID.Set {
		return nil
	}
	if collectionID, ok := cmd.CollectionID.Get(); ok {
		if err := h.requireCollection(ctx, cmd.UserID, collectionID); err != nil {
			return err
		}
	}
	if err := p.Meta.Apply(cmd.Meta, h.now()); err != nil {
		return err
	}
	if cmd.CollectionID.Set {
		p.CollectionID, _ = cmd.CollectionID.Get()
		p.UpdatedAt = h.now()
	}
	return h.Projects.Update(ctx, p)
}

// DeleteProject deletes the project together with its todos, atomically.
type DeleteProject struct {
	UserID, ID string
}

type DeleteProjectHandler struct{ base }

func (h DeleteProjectHandler) Handle(ctx context.Context, cmd DeleteProject) error {
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Todos.DeleteByProject(ctx, cmd.UserID, cmd.ID); err != nil {
			return err
		}
		return apperrors.NotFoundAs(h.Projects.Delete(ctx, cmd.UserID, cmd.ID), domain.ErrProjectNotFound)
	})
}

// ---- collections ----

// CreateCollection adds a collection to group projects.
type CreateCollection struct {
	UserID string
	Meta   domain.MetaChanges // the fields to set; Name is required
}

type CreateCollectionHandler struct{ base }

func (h CreateCollectionHandler) Handle(ctx context.Context, cmd CreateCollection) (id string, err error) {
	meta, err := domain.NewMeta(cmd.Meta, h.now())
	if err != nil {
		return "", err
	}
	c := &domain.Collection{UserID: cmd.UserID, Meta: meta}
	if err := h.Collections.Create(ctx, c); err != nil {
		return "", err
	}
	return c.ID, nil
}

// UpdateCollection renames, restyles or archives a collection.
type UpdateCollection struct {
	UserID, ID string
	Meta       domain.MetaChanges
}

type UpdateCollectionHandler struct{ base }

func (h UpdateCollectionHandler) Handle(ctx context.Context, cmd UpdateCollection) error {
	c, err := h.Collections.Get(ctx, cmd.UserID, cmd.ID)
	if err != nil {
		return apperrors.NotFoundAs(err, domain.ErrCollectionNotFound)
	}
	if cmd.Meta.Empty() {
		return nil
	}
	if err := c.Meta.Apply(cmd.Meta, h.now()); err != nil {
		return err
	}
	return h.Collections.Update(ctx, c)
}

// DeleteCollection deletes the collection only: its projects (and their
// todos) are kept and simply no longer filed under it.
type DeleteCollection struct {
	UserID, ID string
}

type DeleteCollectionHandler struct{ base }

func (h DeleteCollectionHandler) Handle(ctx context.Context, cmd DeleteCollection) error {
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Projects.DetachCollection(ctx, cmd.UserID, cmd.ID); err != nil {
			return err
		}
		return apperrors.NotFoundAs(h.Collections.Delete(ctx, cmd.UserID, cmd.ID), domain.ErrCollectionNotFound)
	})
}
