package memstore_test

import (
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/storetest"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		return storetest.Stores{
			Clients: memstore.NewClients(),
			Codes:   memstore.NewCodes(),
			Grants:  memstore.NewGrants(),
			NewID:   func() string { return bson.NewObjectID().Hex() },
		}
	})
}
