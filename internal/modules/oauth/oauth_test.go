package oauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/auth"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type accounts struct{ userID string }

func (a accounts) Authenticate(context.Context, string) (auth.Principal, error) {
	return auth.Principal{UserID: a.userID, Email: "ann@example.com"}, nil
}

const verifier = "dBjftJeZ4CVP-mJ92K9cX5tRjE6g1nq3ypMPyQKmqZw"

func TestConnectedAppsAreCapped(t *testing.T) {
	ctx := context.Background()
	user := bson.NewObjectID().Hex()
	grants := memstore.NewGrants()
	m := oauth.New(oauth.Deps{
		Clients: memstore.NewClients(), Codes: memstore.NewCodes(), Grants: grants,
		Accounts: accounts{userID: user},
		Config:   oauth.Config{Issuer: "https://api.example.com", Resource: "https://api.example.com/mcp"},
	})
	clientID, err := m.Commands.RegisterClient.Handle(ctx, command.RegisterClient{Name: "Claude", RedirectURIs: []string{"https://claude.ai/cb"}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(verifier))
	req := domain.AuthorizationRequest{
		ResponseType: "code", ClientID: clientID, RedirectURI: "https://claude.ai/cb",
		CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), CodeChallengeMethod: "S256",
	}

	var first command.TokenSet
	for i := range domain.MaxGrantsPerUser + 1 {
		code, err := m.Commands.Approve.Handle(ctx, command.Approve{Request: req, IDToken: "id-token"})
		if err != nil {
			t.Fatal(err)
		}
		tokens, err := m.Commands.ExchangeCode.Handle(ctx, command.ExchangeCode{
			ClientID: clientID, Code: code, RedirectURI: req.RedirectURI, CodeVerifier: verifier,
		})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = tokens
		}
	}
	list, _ := grants.ListForUser(ctx, user)
	if len(list) != domain.MaxGrantsPerUser {
		t.Fatalf("%d grants, want at most %d", len(list), domain.MaxGrantsPerUser)
	}
	if _, err := m.AuthenticateToken(ctx, first.AccessToken); err != oauth.ErrInvalidToken {
		t.Fatalf("the least recently used grant must be revoked: err = %v", err)
	}
}
