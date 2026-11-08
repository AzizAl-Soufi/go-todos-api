package mongostore_test

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/mongostore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/storetest"
)

// Runs against a real MongoDB when TEST_MONGO_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		return storetest.Stores{Store: mongostore.New(dbtest.Mongo(t).Database()), MissingID: bson.NewObjectID().Hex()}
	})
}
