// Package command holds the todos module's write use cases. Each command is a
// plain struct handled by its own handler; a create returns only the new ID,
// everything else is read back through the query side.
package command

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

type Deps struct {
	Todos       application.TodoRepository
	Projects    application.ProjectRepository
	Collections application.CollectionRepository
	Tx          tx.Runner
	Now         func() time.Time // nil means time.Now
}

// Handlers are all todos-module commands.
type Handlers struct {
	CreateTodo       CreateTodoHandler
	UpdateTodo       UpdateTodoHandler
	DeleteTodo       DeleteTodoHandler
	CreateProject    CreateProjectHandler
	UpdateProject    UpdateProjectHandler
	DeleteProject    DeleteProjectHandler
	CreateCollection CreateCollectionHandler
	UpdateCollection UpdateCollectionHandler
	DeleteCollection DeleteCollectionHandler
	DeleteUserData   DeleteUserDataHandler
}

func New(d Deps) Handlers {
	if d.Tx == nil {
		d.Tx = tx.None{}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	b := base{d}
	return Handlers{
		CreateTodo: CreateTodoHandler{b}, UpdateTodo: UpdateTodoHandler{b}, DeleteTodo: DeleteTodoHandler{b},
		CreateProject: CreateProjectHandler{b}, UpdateProject: UpdateProjectHandler{b}, DeleteProject: DeleteProjectHandler{b},
		CreateCollection: CreateCollectionHandler{b}, UpdateCollection: UpdateCollectionHandler{b}, DeleteCollection: DeleteCollectionHandler{b},
		DeleteUserData: DeleteUserDataHandler{b},
	}
}

// base is what every handler shares.
type base struct{ Deps }

func (b base) now() time.Time { return domain.Timestamp(b.Now()) }

// requireProject checks a project referenced by a request. A malformed ID
// can't name an existing project, so it is reported the same way.
func (b base) requireProject(ctx context.Context, userID, id string) error {
	_, err := b.Projects.Get(ctx, userID, id)
	return apperrors.UnresolvedAs(err, domain.ErrProjectNotFound)
}

func (b base) requireCollection(ctx context.Context, userID, id string) error {
	_, err := b.Collections.Get(ctx, userID, id)
	return apperrors.UnresolvedAs(err, domain.ErrCollectionNotFound)
}

// DeleteUserData removes everything the user owns in this module. Called with
// a transactional context (account deletion) it joins that transaction.
type DeleteUserData struct {
	UserID string
}

type DeleteUserDataHandler struct{ base }

func (h DeleteUserDataHandler) Handle(ctx context.Context, cmd DeleteUserData) error {
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Todos.DeleteByUser(ctx, cmd.UserID); err != nil {
			return err
		}
		if err := h.Projects.DeleteByUser(ctx, cmd.UserID); err != nil {
			return err
		}
		return h.Collections.DeleteByUser(ctx, cmd.UserID)
	})
}
