package pgstore_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
)

// Runs against a real PostgreSQL when TEST_POSTGRES_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		pool := dbtest.Postgres(t)
		return storetest.Stores{
			Clients: pgstore.NewClients(pool),
			Codes:   pgstore.NewCodes(pool),
			Grants:  pgstore.NewGrants(pool),
			NewID:   uuid.NewString,
		}
	})
}
