package memstore_test

import (
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Backend {
		return storetest.Backend{
			Events: memstore.NewEvents(),
			Tx:     tx.None{},
			NewID:  func() string { return bson.NewObjectID().Hex() },
		}
	})
}
