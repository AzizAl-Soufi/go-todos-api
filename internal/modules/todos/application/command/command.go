// Package command holds the todos module's write use cases. Each command is a
// plain struct handled by its own handler; a create returns only the new ID,
// everything else is read back through the query side.
//
// Commands run as an access.Actor. Personal records need no role (the scope
// already limits them to their owner); team records need a role in the team:
// editing needs access.Role.CanEdit, deleting projects and collections needs
// CanManage. Changes to team records are written to the team audit log in the
// same unit of work.
package command

import (
	"context"
	"strings"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

type Deps struct {
	Todos       application.TodoRepository
	Projects    application.ProjectRepository
	Collections application.CollectionRepository
	Audit       application.AuditLog // nil records nothing
	Notifier    application.Notifier // nil notifies nobody
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
	DeleteTeamData   DeleteTeamDataHandler
	SendDueAlerts    SendDueAlertsHandler
}

func New(d Deps) Handlers {
	if d.Tx == nil {
		d.Tx = tx.None{}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Audit == nil {
		d.Audit = noAudit{}
	}
	if d.Notifier == nil {
		d.Notifier = noNotifier{}
	}
	b := base{d}
	return Handlers{
		CreateTodo: CreateTodoHandler{b}, UpdateTodo: UpdateTodoHandler{b}, DeleteTodo: DeleteTodoHandler{b},
		CreateProject: CreateProjectHandler{b}, UpdateProject: UpdateProjectHandler{b}, DeleteProject: DeleteProjectHandler{b},
		CreateCollection: CreateCollectionHandler{b}, UpdateCollection: UpdateCollectionHandler{b}, DeleteCollection: DeleteCollectionHandler{b},
		DeleteUserData: DeleteUserDataHandler{b}, DeleteTeamData: DeleteTeamDataHandler{b},
		SendDueAlerts: SendDueAlertsHandler{b},
	}
}

type (
	noAudit    struct{}
	noNotifier struct{}
)

func (noNotifier) TodoCreated(context.Context, application.NewTeamTodo) error { return nil }

func (noNotifier) TodoDue(context.Context, application.TodoDueAlert, time.Time) error { return nil }

func (noAudit) Record(context.Context, application.AuditEntry) error { return nil }

// base is what every handler shares.
type base struct{ Deps }

func (b base) now() time.Time { return domain.Timestamp(b.Now()) }

// authorize checks the actor's role for a record of teamID; personal records
// ("") are the actor's own, the scope guarantees it.
func authorize(a access.Actor, teamID string, allowed func(access.Role) bool) error {
	if teamID == "" {
		return nil
	}
	return a.Require(teamID, allowed)
}

// project resolves a project referenced by a request. A malformed ID can't
// name an existing project, so it is reported the same way.
func (b base) project(ctx context.Context, a access.Actor, id string) (*domain.Project, error) {
	p, err := b.Projects.Get(ctx, application.ScopeOf(a), id)
	return p, apperrors.UnresolvedAs(err, domain.ErrProjectNotFound)
}

func (b base) collection(ctx context.Context, a access.Actor, id string) (*domain.Collection, error) {
	c, err := b.Collections.Get(ctx, application.ScopeOf(a), id)
	return c, apperrors.UnresolvedAs(err, domain.ErrCollectionNotFound)
}

// event describes a change for the team audit log; teamID "" (a personal
// record) means nothing is recorded.
func event(a access.Actor, teamID, action, targetType, targetID string, details map[string]string) application.AuditEntry {
	return application.AuditEntry{TeamID: teamID, ActorID: a.UserID, Action: action, TargetType: targetType, TargetID: targetID, Details: details}
}

func fields(names []string) string { return strings.Join(names, ",") }

// commit runs write and then records the team events, in one unit of work.
// Events are built after write (which assigns IDs); those of personal records
// (no team) are skipped.
func (b base) commit(ctx context.Context, write func(ctx context.Context) error, events ...func() application.AuditEntry) error {
	return b.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := write(ctx); err != nil {
			return err
		}
		for _, describe := range events {
			if e := describe(); e.TeamID != "" {
				if err := b.Audit.Record(ctx, e); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// DeleteUserData removes the user's personal records; team records they
// created stay with the team. Called with a transactional context (account
// deletion) it joins that transaction.
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

// DeleteTeamData removes every record of a team (team deletion).
type DeleteTeamData struct {
	TeamID string
}

type DeleteTeamDataHandler struct{ base }

func (h DeleteTeamDataHandler) Handle(ctx context.Context, cmd DeleteTeamData) error {
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Todos.DeleteByTeam(ctx, cmd.TeamID); err != nil {
			return err
		}
		if err := h.Projects.DeleteByTeam(ctx, cmd.TeamID); err != nil {
			return err
		}
		return h.Collections.DeleteByTeam(ctx, cmd.TeamID)
	})
}
