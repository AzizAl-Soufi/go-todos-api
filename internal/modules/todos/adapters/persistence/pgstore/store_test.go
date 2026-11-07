package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/pgstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
	"github.com/google/uuid"
)

// Runs against a real PostgreSQL when TEST_POSTGRES_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Backend {
		pool := dbtest.Postgres(t)
		return storetest.Backend{
			Todos:       pgstore.NewTodos(pool),
			Projects:    pgstore.NewProjects(pool),
			Collections: pgstore.NewCollections(pool),
			Tx:          postgres.NewTxRunner(pool),
			AtomicTx:    true,
			// Records reference users(id), so owners must exist. The todos
			// module doesn't own users, so the row is inserted directly.
			NewUser: func(t *testing.T) string {
				t.Helper()
				var id uuid.UUID
				err := pool.QueryRow(context.Background(),
					"INSERT INTO users (name, email, created_at) VALUES ('Owner', $1, $2) RETURNING id",
					uuid.NewString()+"@example.com", time.Now()).Scan(&id)
				if err != nil {
					t.Fatalf("insert owner: %v", err)
				}
				return id.String()
			},
			MissingID: uuid.NewString(),
		}
	})
}
