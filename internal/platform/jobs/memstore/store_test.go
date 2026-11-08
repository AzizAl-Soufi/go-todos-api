package memstore_test

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/storetest"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		return storetest.Stores{Store: memstore.New(), MissingID: bson.NewObjectID().Hex()}
	})
}
