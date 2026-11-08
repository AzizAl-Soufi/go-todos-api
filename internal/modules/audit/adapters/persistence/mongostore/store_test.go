package mongostore_test

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/mongostore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

// Runs against a real MongoDB when TEST_MONGO_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Backend {
		client := dbtest.Mongo(t)
		runner := mongodb.NewTxRunner(context.Background(), client)
		_, standalone := runner.(tx.None)
		return storetest.Backend{
			Events:   mongostore.NewEvents(client.Database()),
			Tx:       runner,
			AtomicTx: !standalone,
			NewID:    func() string { return bson.NewObjectID().Hex() },
		}
	})
}
