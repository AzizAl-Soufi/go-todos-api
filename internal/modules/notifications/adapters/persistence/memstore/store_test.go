package memstore_test

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/adapters/persistence/storetest"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		return storetest.Stores{
			Devices: memstore.NewDevices(),
			NewID:   func() string { return bson.NewObjectID().Hex() },
		}
	})
}
