package versions

import (
	"context"
	"slices"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb/migrations"
)

// Teams (teams module), the audit log (audit module) and team ownership of
// collections, projects and todos (todos module). Records without a teamId
// are personal: MongoDB matches a missing field as null, so no backfill is
// needed. Collections are created up front because MongoDB can't create one
// inside a multi-document transaction on older servers.
func init() {
	migrations.Register(5, "teams_audit", upTeamsAudit, downTeamsAudit)
}

var teamsAuditIndexes = map[string][]mongo.IndexModel{
	"teams": nil,
	"team_members": {
		{Keys: bson.D{{Key: "teamId", Value: 1}, {Key: "userId", Value: 1}}, Options: options.Index().SetName("uniq_team_user").SetUnique(true)},
		{Keys: bson.D{{Key: "userId", Value: 1}}, Options: options.Index().SetName("idx_user")},
	},
	"team_invitations": {
		{Keys: bson.D{{Key: "tokenHash", Value: 1}}, Options: options.Index().SetName("uniq_token_hash").SetUnique(true)},
		{Keys: bson.D{{Key: "teamId", Value: 1}, {Key: "email", Value: 1}}, Options: options.Index().SetName("uniq_team_email").SetUnique(true)},
	},
	"audit_events": {
		{Keys: bson.D{{Key: "teamId", Value: 1}, {Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}, Options: options.Index().SetName("idx_team_created")},
		{Keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}, Options: options.Index().SetName("idx_user_created")},
	},
	"collections": {{Keys: bson.D{{Key: "teamId", Value: 1}, {Key: "createdAt", Value: 1}}, Options: options.Index().SetName("idx_team_created")}},
	"projects":    {{Keys: bson.D{{Key: "teamId", Value: 1}, {Key: "createdAt", Value: 1}}, Options: options.Index().SetName("idx_team_created")}},
	"todos":       {{Keys: bson.D{{Key: "teamId", Value: 1}, {Key: "projectId", Value: 1}}, Options: options.Index().SetName("idx_team_project")}},
}

var teamsAuditNewCollections = []string{"teams", "team_members", "team_invitations", "audit_events"}

func upTeamsAudit(db *mongo.Database) error {
	ctx := context.Background()
	existing, err := db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		return err
	}
	for _, name := range teamsAuditNewCollections {
		if !slices.Contains(existing, name) {
			if err := db.CreateCollection(ctx, name); err != nil {
				return err
			}
		}
	}
	for name, indexes := range teamsAuditIndexes {
		if len(indexes) == 0 {
			continue
		}
		if _, err := db.Collection(name).Indexes().CreateMany(ctx, indexes); err != nil {
			return err
		}
	}
	return nil
}

func downTeamsAudit(db *mongo.Database) error {
	ctx := context.Background()
	// Team records can't go back to having no team.
	for name, index := range map[string]string{"todos": "idx_team_project", "projects": "idx_team_created", "collections": "idx_team_created"} {
		if _, err := db.Collection(name).DeleteMany(ctx, bson.M{"teamId": bson.M{"$ne": nil}}); err != nil {
			return err
		}
		if err := db.Collection(name).Indexes().DropOne(ctx, index); err != nil {
			return err
		}
		if _, err := db.Collection(name).UpdateMany(ctx, bson.M{}, bson.M{"$unset": bson.M{"teamId": ""}}); err != nil {
			return err
		}
	}
	for _, name := range teamsAuditNewCollections {
		if err := db.Collection(name).Drop(ctx); err != nil {
			return err
		}
	}
	return nil
}
