package memstore_test

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/storetest"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		s := memstore.New()
		return storetest.Stores{
			Clients: s.Clients,
			Codes:   s.Codes,
			Grants:  s.Grants,
			NewID:   func() string { return bson.NewObjectID().Hex() },
		}
	})
}
