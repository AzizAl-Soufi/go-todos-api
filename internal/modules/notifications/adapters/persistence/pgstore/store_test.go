package pgstore_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
)

// Runs against a real PostgreSQL when TEST_POSTGRES_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		return storetest.Stores{Devices: pgstore.NewDevices(dbtest.Postgres(t)), NewID: uuid.NewString}
	})
}
