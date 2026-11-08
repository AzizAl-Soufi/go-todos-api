package mongostore_test

import (
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/mongostore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Runs against a real MongoDB when TEST_MONGO_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		db := dbtest.Mongo(t).Database()
		return storetest.Stores{
			Clients: mongostore.NewClients(db),
			Codes:   mongostore.NewCodes(db),
			Grants:  mongostore.NewGrants(db),
			NewID:   func() string { return bson.NewObjectID().Hex() },
		}
	})
}
