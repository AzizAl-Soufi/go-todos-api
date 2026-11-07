package memstore_test

import (
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Backend {
		return storetest.Backend{
			Teams: memstore.NewTeams(), Members: memstore.NewMembers(), Invitations: memstore.NewInvitations(),
			Tx: tx.None{}, NewID: func() string { return bson.NewObjectID().Hex() }, MissingID: bson.NewObjectID().Hex(),
		}
	})
}
