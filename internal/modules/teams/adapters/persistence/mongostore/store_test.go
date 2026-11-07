package mongostore_test

import (
	"context"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/persistence/mongostore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Runs against a real MongoDB when TEST_MONGO_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Backend {
		client := dbtest.Mongo(t)
		db := client.Database()
		runner := mongodb.NewTxRunner(context.Background(), client)
		_, standalone := runner.(tx.None)
		return storetest.Backend{
			Teams: mongostore.NewTeams(db), Members: mongostore.NewMembers(db), Invitations: mongostore.NewInvitations(db),
			Tx: runner, AtomicTx: !standalone,
			NewID: func() string { return bson.NewObjectID().Hex() }, MissingID: bson.NewObjectID().Hex(),
		}
	})
}
