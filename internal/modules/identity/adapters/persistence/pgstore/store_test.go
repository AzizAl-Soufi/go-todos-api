package pgstore_test

import (
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
	"github.com/google/uuid"
)

// Runs against a real PostgreSQL when TEST_POSTGRES_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		pool := dbtest.Postgres(t)
		return storetest.Stores{
			Users:      pgstore.NewUsers(pool),
			Passkeys:   pgstore.NewPasskeys(pool),
			Challenges: pgstore.NewPasskeyChallenges(pool),
			Tx:         postgres.NewTxRunner(pool),
			AtomicTx:   true,
			MissingID:  uuid.NewString(),
		}
	})
}
