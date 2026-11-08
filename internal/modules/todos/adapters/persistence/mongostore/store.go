// Package mongostore holds the MongoDB todos, projects and collections
// repositories. Calls made with a transactional context (see
// platform/database/mongodb.TxRunner) join it.
package mongostore

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	TodosCollection       = "todos"
	ProjectsCollection    = "projects"
	CollectionsCollection = "collections"
)

// ---- shared helpers ----

// optionalOID parses an ID that may be empty; empty is stored as null.
func optionalOID(id string) (*bson.ObjectID, error) {
	if id == "" {
		return nil, nil
	}
	oid, err := ids.ObjectID(id)
	if err != nil {
		return nil, err
	}
	return &oid, nil
}

func hexOrEmpty(id *bson.ObjectID) string {
	if id == nil {
		return ""
	}
	return id.Hex()
}

// inScope is the filter for domain.Scope: the user's personal documents
// (teamId null or missing) and those of the scope's teams.
func inScope(sc domain.Scope) (bson.M, error) {
	userOID, err := ids.ObjectID(sc.UserID)
	if err != nil {
		return nil, err
	}
	teams := bson.A{}
	for _, id := range sc.TeamIDs {
		oid, err := ids.ObjectID(id)
		if err != nil {
			return nil, err
		}
		teams = append(teams, oid)
	}
	return bson.M{"$or": bson.A{
		bson.M{"teamId": nil, "userId": userOID},
		bson.M{"teamId": bson.M{"$in": teams}},
	}}, nil
}

// byIDInScope matches the document with id if it is in scope.
func byIDInScope(sc domain.Scope, id string) (bson.M, error) {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return nil, err
	}
	filter, err := inScope(sc)
	if err != nil {
		return nil, err
	}
	filter["_id"] = oid
	return filter, nil
}

// ownerFilter narrows to personal documents or one team's.
func ownerFilter(f domain.OwnerFilter) (bson.M, error) {
	switch {
	case f.Personal:
		return bson.M{"teamId": nil}, nil
	case f.TeamID != "":
		oid, err := ids.ObjectID(f.TeamID)
		if err != nil {
			return nil, err
		}
		return bson.M{"teamId": oid}, nil
	}
	return bson.M{}, nil
}

// replaceInScope overwrites a document in scope, reporting ErrNotFound if absent.
func replaceInScope(ctx context.Context, coll *mongo.Collection, sc domain.Scope, id string, doc any) error {
	filter, err := byIDInScope(sc, id)
	if err != nil {
		return err
	}
	res, err := coll.ReplaceOne(ctx, filter, doc)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func findInScope[T any](ctx context.Context, coll *mongo.Collection, sc domain.Scope, id string) (*T, error) {
	filter, err := byIDInScope(sc, id)
	if err != nil {
		return nil, err
	}
	var doc T
	if err := coll.FindOne(ctx, filter).Decode(&doc); err != nil {
		return nil, mongodb.MapNotFound(err)
	}
	return &doc, nil
}

func deleteInScope(ctx context.Context, coll *mongo.Collection, sc domain.Scope, id string) error {
	filter, err := byIDInScope(sc, id)
	if err != nil {
		return err
	}
	return mongodb.RequireDeleted(coll.DeleteOne(ctx, filter))
}

// deletePersonal removes the user's personal documents.
func deletePersonal(ctx context.Context, coll *mongo.Collection, userID string) error {
	userOID, err := ids.ObjectID(userID)
	if err != nil {
		return err
	}
	_, err = coll.DeleteMany(ctx, bson.M{"userId": userOID, "teamId": nil})
	return err
}

func deleteTeam(ctx context.Context, coll *mongo.Collection, teamID string) error {
	teamOID, err := ids.ObjectID(teamID)
	if err != nil {
		return err
	}
	_, err = coll.DeleteMany(ctx, bson.M{"teamId": teamOID})
	return err
}

// ---- todos ----

type Todos struct{ coll *mongo.Collection }

var _ application.TodoStore = (*Todos)(nil)

func NewTodos(db *mongo.Database) *Todos { return &Todos{coll: db.Collection(TodosCollection)} }

type todoDoc struct {
	ID          bson.ObjectID  `bson:"_id"`
	UserID      bson.ObjectID  `bson:"userId"`
	TeamID      *bson.ObjectID `bson:"teamId"`
	ProjectID   *bson.ObjectID `bson:"projectId"`
	Title       string         `bson:"title"`
	Notes       string         `bson:"notes"`
	Status      string         `bson:"status"`
	Priority    int            `bson:"priority"`
	Tags        []string       `bson:"tags"`
	DueAt       *time.Time     `bson:"dueAt"`
	CompletedAt *time.Time     `bson:"completedAt"`
	CreatedAt   time.Time      `bson:"createdAt"`
	UpdatedAt   time.Time      `bson:"updatedAt"`
	// The due-date alert state: the deadline each alert was sent for. Only
	// MarkDueAlerted writes it; Create and Update leave it out.
	DueSoonAlertedFor *time.Time `bson:"dueSoonAlertedFor,omitempty"`
	OverdueAlertedFor *time.Time `bson:"overdueAlertedFor,omitempty"`
}

const (
	dueSoonAlertedField = "dueSoonAlertedFor"
	overdueAlertedField = "overdueAlertedFor"
)

func newTodoDoc(t *domain.Todo, id bson.ObjectID) (*todoDoc, error) {
	userOID, err := ids.ObjectID(t.UserID)
	if err != nil {
		return nil, err
	}
	teamOID, err := optionalOID(t.TeamID)
	if err != nil {
		return nil, err
	}
	projectOID, err := optionalOID(t.ProjectID)
	if err != nil {
		return nil, err
	}
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	return &todoDoc{
		ID: id, UserID: userOID, TeamID: teamOID, ProjectID: projectOID, Title: t.Title, Notes: t.Notes,
		Status: string(t.Status), Priority: int(t.Priority), Tags: tags, DueAt: t.DueAt,
		CompletedAt: t.CompletedAt, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}, nil
}

func (d *todoDoc) toDomain() *domain.Todo {
	status := domain.Status(d.Status)
	if status == "" { // written before statuses existed and not migrated
		status = domain.StatusPending
	}
	tags := d.Tags
	if tags == nil {
		tags = []string{}
	}
	return &domain.Todo{
		ID: d.ID.Hex(), UserID: d.UserID.Hex(), TeamID: hexOrEmpty(d.TeamID), ProjectID: hexOrEmpty(d.ProjectID), Title: d.Title, Notes: d.Notes,
		Status: status, Priority: domain.Priority(d.Priority), Tags: tags, DueAt: d.DueAt,
		CompletedAt: d.CompletedAt, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

func (s *Todos) Create(ctx context.Context, t *domain.Todo) error {
	doc, err := newTodoDoc(t, bson.NewObjectID())
	if err != nil {
		return err
	}
	if _, err := s.coll.InsertOne(ctx, doc); err != nil {
		return err
	}
	t.ID = doc.ID.Hex()
	return nil
}

func (s *Todos) Get(ctx context.Context, sc domain.Scope, id string) (*domain.Todo, error) {
	doc, err := findInScope[todoDoc](ctx, s.coll, sc, id)
	if err != nil {
		return nil, err
	}
	return doc.toDomain(), nil
}

func (s *Todos) Update(ctx context.Context, sc domain.Scope, t *domain.Todo) error {
	oid, err := ids.ObjectID(t.ID)
	if err != nil {
		return err
	}
	doc, err := newTodoDoc(t, oid)
	if err != nil {
		return err
	}
	// $set rather than a replacement, which would drop the alert state.
	fields, err := setFields(doc)
	if err != nil {
		return err
	}
	filter, err := byIDInScope(sc, t.ID)
	if err != nil {
		return err
	}
	res, err := s.coll.UpdateOne(ctx, filter, bson.M{"$set": fields})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

// setFields is doc's fields for $set: all but _id (immutable) and those
// left empty (the alert state).
func setFields(doc *todoDoc) (bson.D, error) {
	raw, err := bson.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var all bson.D
	if err := bson.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	out := make(bson.D, 0, len(all))
	for _, e := range all {
		if e.Key != "_id" {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *Todos) Delete(ctx context.Context, sc domain.Scope, id string) error {
	return deleteInScope(ctx, s.coll, sc, id)
}

var closedStatuses = bson.A{string(domain.StatusDone), string(domain.StatusCancelled)}

// filter translates domain.Query, within a scope, into a MongoDB filter.
func filter(sc domain.Scope, q domain.Query) (bson.M, error) {
	scope, err := inScope(sc)
	if err != nil {
		return nil, err
	}
	owner, err := ownerFilter(q.OwnerFilter)
	if err != nil {
		return nil, err
	}
	and := bson.A{scope, owner}
	if q.ProjectID != "" {
		oid, err := ids.ObjectID(q.ProjectID)
		if err != nil {
			return nil, err
		}
		and = append(and, bson.M{"projectId": oid})
	}
	if q.Inbox {
		and = append(and, bson.M{"projectId": nil})
	}
	if len(q.ExcludeProjectIDs) > 0 {
		excluded := bson.A{}
		for _, id := range q.ExcludeProjectIDs {
			oid, err := ids.ObjectID(id)
			if err != nil {
				return nil, err
			}
			excluded = append(excluded, oid)
		}
		and = append(and, bson.M{"projectId": bson.M{"$nin": excluded}})
	}
	if len(q.Statuses) > 0 {
		statuses := bson.A{}
		for _, st := range q.Statuses {
			statuses = append(statuses, string(st))
		}
		and = append(and, bson.M{"status": bson.M{"$in": statuses}})
	}
	if len(q.Priorities) > 0 {
		priorities := bson.A{}
		for _, p := range q.Priorities {
			priorities = append(priorities, int(p))
		}
		and = append(and, bson.M{"priority": bson.M{"$in": priorities}})
	}
	if q.Tag != "" {
		and = append(and, bson.M{"tags": q.Tag})
	}
	// Comparisons with a date never match null, like SQL and domain.Query.
	if q.DueBefore != nil {
		and = append(and, bson.M{"dueAt": bson.M{"$lt": *q.DueBefore}})
	}
	if q.DueAfter != nil {
		and = append(and, bson.M{"dueAt": bson.M{"$gte": *q.DueAfter}})
	}
	if q.OverdueAt != nil {
		and = append(and, bson.M{"dueAt": bson.M{"$lt": *q.OverdueAt}, "status": bson.M{"$nin": closedStatuses}})
	}
	if q.Search != "" {
		re := bson.Regex{Pattern: regexp.QuoteMeta(q.Search), Options: "i"}
		and = append(and, bson.M{"$or": bson.A{bson.M{"title": re}, bson.M{"notes": re}}})
	}
	return bson.M{"$and": and}, nil
}

// sortStage orders like domain.Query.Compare. MongoDB sorts missing values
// first, so "no deadline last" sorts on a computed noDue flag.
func sortStage(sort domain.Sort) bson.D {
	newest := bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}
	due := bson.D{{Key: "noDue", Value: 1}, {Key: "dueAt", Value: 1}}
	switch sort {
	case domain.SortUpdated:
		return append(bson.D{{Key: "updatedAt", Value: -1}}, newest...)
	case domain.SortDue:
		return append(due, newest...)
	case domain.SortPriority:
		return append(append(bson.D{{Key: "priority", Value: -1}}, due...), newest...)
	default:
		return newest
	}
}

func (s *Todos) List(ctx context.Context, sc domain.Scope, q domain.Query) ([]*domain.Todo, error) {
	match, err := filter(sc, q)
	if err != nil {
		return nil, err
	}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$addFields", Value: bson.M{"noDue": bson.M{"$cond": bson.A{bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$dueAt", nil}}, nil}}, 1, 0}}}}},
		{{Key: "$sort", Value: sortStage(q.Sort)}},
		{{Key: "$unset", Value: "noDue"}},
	}
	cursor, err := s.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	var docs []todoDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]*domain.Todo, 0, len(docs))
	for i := range docs {
		out = append(out, docs[i].toDomain())
	}
	return out, nil
}

func (s *Todos) Progress(ctx context.Context, sc domain.Scope, now time.Time) (map[string]domain.Progress, error) {
	scope, err := inScope(sc)
	if err != nil {
		return nil, err
	}
	hasDue := bson.M{"$ne": bson.A{bson.M{"$ifNull": bson.A{"$dueAt", nil}}, nil}}
	overdue := bson.M{"$and": bson.A{
		hasDue,
		bson.M{"$lt": bson.A{"$dueAt", now}},
		bson.M{"$not": bson.A{bson.M{"$in": bson.A{"$status", closedStatuses}}}},
	}}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: scope}},
		{{Key: "$group", Value: bson.M{
			"_id":     bson.M{"project": "$projectId", "status": bson.M{"$ifNull": bson.A{"$status", string(domain.StatusPending)}}},
			"total":   bson.M{"$sum": 1},
			"overdue": bson.M{"$sum": bson.M{"$cond": bson.A{overdue, 1, 0}}},
		}}},
	}
	cursor, err := s.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID struct {
			Project *bson.ObjectID `bson:"project"`
			Status  string         `bson:"status"`
		} `bson:"_id"`
		Total   int `bson:"total"`
		Overdue int `bson:"overdue"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := map[string]domain.Progress{}
	for _, r := range rows {
		key := hexOrEmpty(r.ID.Project)
		p := out[key]
		p.AddCount(domain.Status(r.ID.Status), r.Total, r.Overdue)
		out[key] = p
	}
	return out, nil
}

func (s *Todos) DeleteByProject(ctx context.Context, sc domain.Scope, projectID string) error {
	projectOID, err := ids.ObjectID(projectID)
	if err != nil {
		return err
	}
	filter, err := inScope(sc)
	if err != nil {
		return err
	}
	filter["projectId"] = projectOID
	_, err = s.coll.DeleteMany(ctx, filter)
	return err
}

func (s *Todos) DeleteByUser(ctx context.Context, userID string) error {
	return deletePersonal(ctx, s.coll, userID)
}

func (s *Todos) DeleteByTeam(ctx context.Context, teamID string) error {
	return deleteTeam(ctx, s.coll, teamID)
}

var openStatuses = bson.A{string(domain.StatusPending), string(domain.StatusInProgress), string(domain.StatusBlocked)}

// ListPendingDueAlertsSystemWide is system-level: not scoped (see
// application.DueAlertRepository). The match narrows to the candidates on
// idx_due_alerts; domain.NextDueAlert then decides each, so the semantics
// can't drift from the other stores.
func (s *Todos) ListPendingDueAlertsSystemWide(ctx context.Context, now time.Time, limit int) ([]application.DueAlert, error) {
	out := make([]application.DueAlert, 0)
	if limit <= 0 {
		return out, nil
	}
	notSentFor := func(field string) bson.M { return bson.M{"$ne": bson.A{"$" + field, "$dueAt"}} }
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{
			"status": bson.M{"$in": openStatuses},
			"dueAt":  bson.M{"$type": "date", "$lte": now.Add(domain.DueSoonWindow)},
			"$expr": bson.M{"$and": bson.A{
				notSentFor(overdueAlertedField),
				bson.M{"$or": bson.A{bson.M{"$lt": bson.A{"$dueAt", now}}, notSentFor(dueSoonAlertedField)}},
			}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "dueAt", Value: 1}, {Key: "_id", Value: 1}}}},
		// Leave out todos of archived projects.
		{{Key: "$lookup", Value: bson.M{
			"from": ProjectsCollection, "localField": "projectId", "foreignField": "_id", "as": "archivedProject",
			"pipeline": bson.A{
				bson.M{"$match": bson.M{"archivedAt": bson.M{"$ne": nil}}},
				bson.M{"$project": bson.M{"_id": 1}},
			},
		}}},
		{{Key: "$match", Value: bson.M{"archivedProject": bson.M{"$size": 0}}}},
		{{Key: "$limit", Value: limit}},
		{{Key: "$unset", Value: "archivedProject"}},
	}
	cursor, err := s.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	var docs []todoDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	for i := range docs {
		todo := docs[i].toDomain()
		sent := domain.DueAlertsSent{DueSoonFor: docs[i].DueSoonAlertedFor, OverdueFor: docs[i].OverdueAlertedFor}
		if kind, ok := domain.NextDueAlert(todo, sent, now); ok {
			out = append(out, application.DueAlert{Todo: todo, Kind: kind})
		}
	}
	return out, nil
}

// MarkDueAlerted is system-level: not scoped (see
// application.DueAlertRepository).
func (s *Todos) MarkDueAlerted(ctx context.Context, id string, kind domain.DueAlertKind, dueAt time.Time) (bool, error) {
	oid, err := ids.ObjectID(id)
	if err != nil {
		return false, err
	}
	var field string
	switch kind {
	case domain.AlertDueSoon:
		field = dueSoonAlertedField
	case domain.AlertOverdue:
		field = overdueAlertedField
	default:
		return false, fmt.Errorf("mark due alerted: unknown kind %q", kind)
	}
	res, err := s.coll.UpdateOne(ctx,
		bson.M{"_id": oid, "dueAt": dueAt, "status": bson.M{"$in": openStatuses}, field: bson.M{"$ne": dueAt}},
		bson.M{"$set": bson.M{field: dueAt}})
	if err != nil {
		return false, err
	}
	return res.ModifiedCount > 0, nil
}

// ---- projects and collections ----

// MetaDoc is the stored form of domain.Meta, inlined into project and
// collection documents. It must be exported: the BSON encoder skips
// unexported embedded structs, which would silently drop these fields.
type MetaDoc struct {
	Name        string     `bson:"name"`
	Description string     `bson:"description"`
	Icon        string     `bson:"icon"`
	Color       string     `bson:"color"`
	ArchivedAt  *time.Time `bson:"archivedAt"`
	CreatedAt   time.Time  `bson:"createdAt"`
	UpdatedAt   time.Time  `bson:"updatedAt"`
}

func toMetaDoc(m domain.Meta) MetaDoc {
	return MetaDoc{Name: m.Name, Description: m.Description, Icon: m.Icon, Color: m.Color, ArchivedAt: m.ArchivedAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
}

func (d MetaDoc) toDomain() domain.Meta {
	return domain.Meta{Name: d.Name, Description: d.Description, Icon: d.Icon, Color: d.Color, ArchivedAt: d.ArchivedAt, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
}

func archiveFilter(f domain.ArchiveFilter) bson.M {
	switch f {
	case domain.ActiveOnly:
		return bson.M{"archivedAt": nil}
	case domain.ArchivedOnly:
		return bson.M{"archivedAt": bson.M{"$ne": nil}}
	default:
		return bson.M{}
	}
}

var oldestFirst = options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}})

func findAll[T any](ctx context.Context, coll *mongo.Collection, filter bson.M) ([]T, error) {
	cursor, err := coll.Find(ctx, filter, oldestFirst)
	if err != nil {
		return nil, err
	}
	var docs []T
	err = cursor.All(ctx, &docs)
	return docs, err
}

type Projects struct{ coll *mongo.Collection }

var _ application.ProjectStore = (*Projects)(nil)

func NewProjects(db *mongo.Database) *Projects {
	return &Projects{coll: db.Collection(ProjectsCollection)}
}

type projectDoc struct {
	ID           bson.ObjectID  `bson:"_id"`
	UserID       bson.ObjectID  `bson:"userId"`
	TeamID       *bson.ObjectID `bson:"teamId"`
	CollectionID *bson.ObjectID `bson:"collectionId"`
	MetaDoc      `bson:",inline"`
}

func newProjectDoc(p *domain.Project, id bson.ObjectID) (*projectDoc, error) {
	userOID, err := ids.ObjectID(p.UserID)
	if err != nil {
		return nil, err
	}
	teamOID, err := optionalOID(p.TeamID)
	if err != nil {
		return nil, err
	}
	collectionOID, err := optionalOID(p.CollectionID)
	if err != nil {
		return nil, err
	}
	return &projectDoc{ID: id, UserID: userOID, TeamID: teamOID, CollectionID: collectionOID, MetaDoc: toMetaDoc(p.Meta)}, nil
}

func (d *projectDoc) toDomain() *domain.Project {
	return &domain.Project{ID: d.ID.Hex(), UserID: d.UserID.Hex(), TeamID: hexOrEmpty(d.TeamID), CollectionID: hexOrEmpty(d.CollectionID), Meta: d.MetaDoc.toDomain()}
}

func (s *Projects) Create(ctx context.Context, p *domain.Project) error {
	doc, err := newProjectDoc(p, bson.NewObjectID())
	if err != nil {
		return err
	}
	if _, err := s.coll.InsertOne(ctx, doc); err != nil {
		return err
	}
	p.ID = doc.ID.Hex()
	return nil
}

func (s *Projects) Get(ctx context.Context, sc domain.Scope, id string) (*domain.Project, error) {
	doc, err := findInScope[projectDoc](ctx, s.coll, sc, id)
	if err != nil {
		return nil, err
	}
	return doc.toDomain(), nil
}

func (s *Projects) Update(ctx context.Context, sc domain.Scope, p *domain.Project) error {
	oid, err := ids.ObjectID(p.ID)
	if err != nil {
		return err
	}
	doc, err := newProjectDoc(p, oid)
	if err != nil {
		return err
	}
	return replaceInScope(ctx, s.coll, sc, p.ID, doc)
}

func (s *Projects) Delete(ctx context.Context, sc domain.Scope, id string) error {
	return deleteInScope(ctx, s.coll, sc, id)
}

// listFilter combines scope, owner filter and archive state.
func listFilter(sc domain.Scope, owner domain.OwnerFilter, archive domain.ArchiveFilter) (bson.M, error) {
	scope, err := inScope(sc)
	if err != nil {
		return nil, err
	}
	o, err := ownerFilter(owner)
	if err != nil {
		return nil, err
	}
	return bson.M{"$and": bson.A{scope, o, archiveFilter(archive)}}, nil
}

func (s *Projects) List(ctx context.Context, sc domain.Scope, f domain.ProjectFilter) ([]*domain.Project, error) {
	match, err := listFilter(sc, f.OwnerFilter, f.Archive)
	if err != nil {
		return nil, err
	}
	if f.CollectionID != "" {
		oid, err := ids.ObjectID(f.CollectionID)
		if err != nil {
			return nil, err
		}
		match["collectionId"] = oid
	}
	docs, err := findAll[projectDoc](ctx, s.coll, match)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Project, 0, len(docs))
	for i := range docs {
		out = append(out, docs[i].toDomain())
	}
	return out, nil
}

func (s *Projects) DetachCollection(ctx context.Context, sc domain.Scope, collectionID string) error {
	collectionOID, err := ids.ObjectID(collectionID)
	if err != nil {
		return err
	}
	filter, err := inScope(sc)
	if err != nil {
		return err
	}
	filter["collectionId"] = collectionOID
	_, err = s.coll.UpdateMany(ctx, filter, bson.M{"$set": bson.M{"collectionId": nil}})
	return err
}

func (s *Projects) DeleteByUser(ctx context.Context, userID string) error {
	return deletePersonal(ctx, s.coll, userID)
}

func (s *Projects) DeleteByTeam(ctx context.Context, teamID string) error {
	return deleteTeam(ctx, s.coll, teamID)
}

type Collections struct{ coll *mongo.Collection }

var _ application.CollectionStore = (*Collections)(nil)

func NewCollections(db *mongo.Database) *Collections {
	return &Collections{coll: db.Collection(CollectionsCollection)}
}

type collectionDoc struct {
	ID      bson.ObjectID  `bson:"_id"`
	UserID  bson.ObjectID  `bson:"userId"`
	TeamID  *bson.ObjectID `bson:"teamId"`
	MetaDoc `bson:",inline"`
}

func newCollectionDoc(c *domain.Collection, id bson.ObjectID) (*collectionDoc, error) {
	userOID, err := ids.ObjectID(c.UserID)
	if err != nil {
		return nil, err
	}
	teamOID, err := optionalOID(c.TeamID)
	if err != nil {
		return nil, err
	}
	return &collectionDoc{ID: id, UserID: userOID, TeamID: teamOID, MetaDoc: toMetaDoc(c.Meta)}, nil
}

func (d *collectionDoc) toDomain() *domain.Collection {
	return &domain.Collection{ID: d.ID.Hex(), UserID: d.UserID.Hex(), TeamID: hexOrEmpty(d.TeamID), Meta: d.MetaDoc.toDomain()}
}

func (s *Collections) Create(ctx context.Context, c *domain.Collection) error {
	doc, err := newCollectionDoc(c, bson.NewObjectID())
	if err != nil {
		return err
	}
	if _, err := s.coll.InsertOne(ctx, doc); err != nil {
		return err
	}
	c.ID = doc.ID.Hex()
	return nil
}

func (s *Collections) Get(ctx context.Context, sc domain.Scope, id string) (*domain.Collection, error) {
	doc, err := findInScope[collectionDoc](ctx, s.coll, sc, id)
	if err != nil {
		return nil, err
	}
	return doc.toDomain(), nil
}

func (s *Collections) Update(ctx context.Context, sc domain.Scope, c *domain.Collection) error {
	oid, err := ids.ObjectID(c.ID)
	if err != nil {
		return err
	}
	doc, err := newCollectionDoc(c, oid)
	if err != nil {
		return err
	}
	return replaceInScope(ctx, s.coll, sc, c.ID, doc)
}

func (s *Collections) Delete(ctx context.Context, sc domain.Scope, id string) error {
	return deleteInScope(ctx, s.coll, sc, id)
}

func (s *Collections) List(ctx context.Context, sc domain.Scope, f domain.CollectionFilter) ([]*domain.Collection, error) {
	match, err := listFilter(sc, f.OwnerFilter, f.Archive)
	if err != nil {
		return nil, err
	}
	docs, err := findAll[collectionDoc](ctx, s.coll, match)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Collection, 0, len(docs))
	for i := range docs {
		out = append(out, docs[i].toDomain())
	}
	return out, nil
}

func (s *Collections) DeleteByUser(ctx context.Context, userID string) error {
	return deletePersonal(ctx, s.coll, userID)
}

func (s *Collections) DeleteByTeam(ctx context.Context, teamID string) error {
	return deleteTeam(ctx, s.coll, teamID)
}
