package mail

import (
	"context"
	"strings"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
)

type captured struct{ to, subject, body string }

func (c *captured) Send(_ context.Context, to, subject, body string) error {
	*c = captured{to, subject, body}
	return nil
}

func TestMailer(t *testing.T) {
	var c captured
	err := Mailer{Sender: &c}.SendInvitation(context.Background(), "bo@example.com", "Ann", "Plat\r\nform", access.Member, "https://x.org/invitations/accept?token=abc")
	if err != nil {
		t.Fatal(err)
	}
	if c.to != "bo@example.com" || strings.ContainsAny(c.subject, "\r\n") || !strings.Contains(c.body, "Ann invited you") ||
		!strings.Contains(c.body, "as member") || !strings.Contains(c.body, "token=abc") {
		t.Fatalf("email = %+v", c)
	}
}
