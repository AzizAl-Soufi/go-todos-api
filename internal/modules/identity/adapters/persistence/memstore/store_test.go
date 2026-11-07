package memstore_test

import (
	"context"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/storetest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) storetest.Stores {
		return storetest.Stores{
			Users:          memstore.NewUsers(),
			Sessions:       memstore.NewSessions(),
			Verifications:  memstore.NewVerifications(),
			PasswordResets: memstore.NewPasswordResets(),
			Tx:             tx.None{},
			MissingID:      "missing",
		}
	})
}

func TestUsers_CopiesValues(t *testing.T) {
	ctx := context.Background()
	s := memstore.NewUsers()
	u := &domain.User{Name: "Ann", Email: "ann@example.com"}
	if err := s.Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	got, _ := s.FindByEmail(ctx, "ann@example.com")
	got.Name = "Mallory"
	u.Name = "Mallory"
	if fresh, _ := s.FindByID(ctx, u.ID); fresh.Name != "Ann" {
		t.Fatal("stored user must not share memory with values handed out or passed in")
	}
}
