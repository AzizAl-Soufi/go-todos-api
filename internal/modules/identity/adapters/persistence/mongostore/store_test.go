package mongostore_test

import (
	"context"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/mongostore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/dbtest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/mongodb"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Runs against a real MongoDB when TEST_MONGO_URI is set (see dbtest).
func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		client := dbtest.Mongo(t)
		db := client.Database()
		runner := mongodb.NewTxRunner(context.Background(), client)
		_, standalone := runner.(tx.None)
		return storetest.Stores{
			Users:          mongostore.NewUsers(db),
			Sessions:       mongostore.NewSessions(db),
			Verifications:  mongostore.NewVerifications(db),
			PasswordResets: mongostore.NewPasswordResets(db),
			Passkeys:       mongostore.NewPasskeys(db),
			Challenges:     mongostore.NewPasskeyChallenges(db),
			Tx:             runner,
			AtomicTx:       !standalone,
			MissingID:      bson.NewObjectID().Hex(),
		}
	})
}
