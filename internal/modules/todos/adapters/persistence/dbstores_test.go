package persistence_test

// IDs in the wrong format are rejected with ErrInvalidID before the stores
// touch the (nil) connection. Behaviour against real databases is covered by
// the storetest contract in mongostore and pgstore.

import (
	"context"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/mongostore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/testutil"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func invalidIDCases(todos application.TodoStore, projects application.ProjectStore, collections application.CollectionStore, valid, foreign string) map[string]func() error {
	ctx := context.Background()
	scope := func(userID string) domain.Scope { return domain.Scope{UserID: userID} }
	project := func(id, user, collection string) *domain.Project {
		return &domain.Project{ID: id, UserID: user, CollectionID: collection}
	}
	return map[string]func() error{
		"Create todo bad user":    func() error { return todos.Create(ctx, &domain.Todo{UserID: "bad"}) },
		"Create todo bad project": func() error { return todos.Create(ctx, &domain.Todo{UserID: valid, ProjectID: "bad"}) },
		"Get todo bad id":         func() error { _, err := todos.Get(ctx, scope(valid), "bad"); return err },
		"Get todo bad user":       func() error { _, err := todos.Get(ctx, scope("bad"), valid); return err },
		"Get todo bad team": func() error {
			_, err := todos.Get(ctx, domain.Scope{UserID: valid, TeamIDs: []string{"bad"}}, valid)
			return err
		},
		"Get todo other id format": func() error { _, err := todos.Get(ctx, scope(valid), foreign); return err },
		"Update todo bad id":       func() error { return todos.Update(ctx, scope(valid), &domain.Todo{ID: "bad", UserID: valid}) },
		"Create todo bad team":     func() error { return todos.Create(ctx, &domain.Todo{UserID: valid, TeamID: "bad"}) },
		"Delete todo bad user":     func() error { return todos.Delete(ctx, scope("bad"), valid) },
		"List bad user":            func() error { _, err := todos.List(ctx, scope("bad"), domain.Query{}); return err },
		"List bad team filter": func() error {
			_, err := todos.List(ctx, scope(valid), domain.Query{OwnerFilter: domain.OwnerFilter{TeamID: "bad"}})
			return err
		},
		"List bad project filter": func() error { _, err := todos.List(ctx, scope(valid), domain.Query{ProjectID: "bad"}); return err },
		"List bad excluded project": func() error {
			_, err := todos.List(ctx, scope(valid), domain.Query{ExcludeProjectIDs: []string{"bad"}})
			return err
		},
		"Progress bad user":       func() error { _, err := todos.Progress(ctx, scope("bad"), time.Now()); return err },
		"DeleteByProject bad id":  func() error { return todos.DeleteByProject(ctx, scope(valid), "bad") },
		"DeleteByTeam todos":      func() error { return todos.DeleteByTeam(ctx, "bad") },
		"DeleteByUser todos":      func() error { return todos.DeleteByUser(ctx, "bad") },
		"Create project bad user": func() error { return projects.Create(ctx, project("", "bad", "")) },
		"Create project bad coll": func() error { return projects.Create(ctx, project("", valid, "bad")) },
		"Get project bad id":      func() error { _, err := projects.Get(ctx, scope(valid), "bad"); return err },
		"Update project bad id":   func() error { return projects.Update(ctx, scope(valid), project("bad", valid, "")) },
		"Delete project bad id":   func() error { return projects.Delete(ctx, scope(valid), "bad") },
		"List projects bad coll": func() error {
			_, err := projects.List(ctx, scope(valid), domain.ProjectFilter{CollectionID: "bad"})
			return err
		},
		"DetachCollection bad id":    func() error { return projects.DetachCollection(ctx, scope(valid), "bad") },
		"DeleteByTeam projects":      func() error { return projects.DeleteByTeam(ctx, "bad") },
		"DeleteByUser projects":      func() error { return projects.DeleteByUser(ctx, "bad") },
		"Create collection bad user": func() error { return collections.Create(ctx, &domain.Collection{UserID: "bad"}) },
		"Get collection bad id":      func() error { _, err := collections.Get(ctx, scope(valid), "bad"); return err },
		"Delete collection bad id":   func() error { return collections.Delete(ctx, scope(valid), "bad") },
		"Create collection bad team": func() error { return collections.Create(ctx, &domain.Collection{UserID: valid, TeamID: "bad"}) },
		"DeleteByTeam collections":   func() error { return collections.DeleteByTeam(ctx, "bad") },
		"List collections bad user":  func() error { _, err := collections.List(ctx, scope("bad"), domain.CollectionFilter{}); return err },
	}
}

func TestMongoStores_RejectInvalidIDs(t *testing.T) {
	// Nil collections: reaching MongoDB would panic.
	cases := invalidIDCases(&mongostore.Todos{}, &mongostore.Projects{}, &mongostore.Collections{}, bson.NewObjectID().Hex(), uuid.NewString())
	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, cases)
}

func TestPostgresStores_RejectInvalidIDs(t *testing.T) {
	// Nil pool: reaching PostgreSQL would panic.
	cases := invalidIDCases(pgstore.NewTodos(nil), pgstore.NewProjects(nil), pgstore.NewCollections(nil), uuid.NewString(), bson.NewObjectID().Hex())
	testutil.ExpectErrorIs(t, apperrors.ErrInvalidID, cases)
}
