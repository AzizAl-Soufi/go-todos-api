package pgstore_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/storetest"
)

// Runs against a real PostgreSQL when TEST_POSTGRES_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		return storetest.Stores{Store: pgstore.New(dbtest.Postgres(t)), MissingID: uuid.NewString()}
	})
}
