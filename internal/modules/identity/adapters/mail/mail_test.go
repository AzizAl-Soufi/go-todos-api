package mail

import (
	"context"
	"strings"
	"testing"
)

type captured struct{ to, subject, body string }

func (c *captured) Send(_ context.Context, to, subject, body string) error {
	*c = captured{to, subject, body}
	return nil
}

func TestMailer(t *testing.T) {
	var c captured
	m := Mailer{Sender: &c}

	if err := m.SendVerificationEmail(context.Background(), "ann@example.com", "Ann", "https://x.org/verify?token=abc"); err != nil {
		t.Fatal(err)
	}
	if c.to != "ann@example.com" || c.subject != verificationSubject || !strings.Contains(c.body, "Hi Ann,") || !strings.Contains(c.body, "token=abc") {
		t.Fatalf("verification email = %+v", c)
	}

	if err := m.SendPasswordResetEmail(context.Background(), "ann@example.com", "Ann\r\nBcc: x", "https://x.org/reset-password?token=abc"); err != nil {
		t.Fatal(err)
	}
	if c.subject != passwordResetSubject || !strings.Contains(c.body, "reset-password?token=abc") || strings.Contains(c.body, "Ann\r\nBcc") {
		t.Fatalf("reset email = %+v", c)
	}
}
