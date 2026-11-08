package pgstore_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/audit/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
)

// Runs against a real PostgreSQL when TEST_POSTGRES_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Backend {
		pool := dbtest.Postgres(t)
		return storetest.Backend{
			Events:   pgstore.NewEvents(pool),
			Tx:       postgres.NewTxRunner(pool),
			AtomicTx: true,
			NewID:    uuid.NewString,
		}
	})
}
