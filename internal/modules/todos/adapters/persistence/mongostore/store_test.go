package mongostore_test

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/mongostore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

// Runs against a real MongoDB when TEST_MONGO_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Backend {
		client := dbtest.Mongo(t)
		db := client.Database()
		runner := mongodb.NewTxRunner(context.Background(), client)
		_, standalone := runner.(tx.None)
		return storetest.Backend{
			Todos:       mongostore.NewTodos(db),
			Projects:    mongostore.NewProjects(db),
			Collections: mongostore.NewCollections(db),
			Tx:          runner,
			AtomicTx:    !standalone,
			NewUser:     func(*testing.T) string { return bson.NewObjectID().Hex() },
			NewID:       func() string { return bson.NewObjectID().Hex() },
			MissingID:   bson.NewObjectID().Hex(),
		}
	})
}
