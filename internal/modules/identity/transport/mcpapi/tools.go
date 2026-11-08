// Package mcpapi is the identity module's MCP transport: the caller's
// profile. Account-level actions (deleting the account, logging out
// everywhere, passkeys, sign-in) are deliberately not tools.
package mcpapi

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/transport/httpapi"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/mcpx"
)

// Register adds the identity tools to s.
func Register(s *mcp.Server, qry query.Handlers) {
	mcpx.Add(s, &mcp.Tool{
		Name:        "get_profile",
		Description: "Get the signed-in user's account: ID, name, email, whether the email is verified, and when the account was created.",
		Annotations: mcpx.Read("Get my profile"),
	}, func(ctx context.Context, a access.Actor, _ struct{}) (httpapi.UserResponse, error) {
		u, err := qry.GetUser.Handle(ctx, query.GetUser{UserID: a.UserID})
		if err != nil {
			return httpapi.UserResponse{}, err
		}
		return httpapi.ToUser(u), nil
	})
}
