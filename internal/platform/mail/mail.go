// Package mail sends plain-text email. Modules write their own messages
// (in their mail adapters) and hand them to a Sender.
package mail

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Sender delivers one plain-text message.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// LogSender writes emails to the log instead of sending them. For local
// development and tests only: the log contains the links (tokens) in them.
type LogSender struct{}

func (LogSender) Send(_ context.Context, to, subject, body string) error {
	slog.Info("email (not sent: SMTP is not configured)", "to", to, "subject", subject, "body", body)
	return nil
}

// SMTP sends email through an SMTP server. net/smtp upgrades to TLS via
// STARTTLS when the server offers it, and refuses to send credentials without
// TLS (except to localhost).
type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

func (m SMTP) Send(_ context.Context, to, subject, body string) error {
	if err := rejectHeaderInjection(to, m.From, subject); err != nil {
		return err
	}
	msg := BuildMessage(m.From, to, subject, body)

	var auth smtp.Auth
	if m.Username != "" {
		auth = smtp.PlainAuth("", m.Username, m.Password, m.Host)
	}
	addr := net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
	if err := smtp.SendMail(addr, auth, m.From, []string{to}, msg); err != nil {
		return fmt.Errorf("send %q email: %w", subject, err)
	}
	return nil
}

// BuildMessage formats a plain-text UTF-8 message with its headers.
func BuildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

func rejectHeaderInjection(values ...string) error {
	for _, v := range values {
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("invalid email header value %q", v)
		}
	}
	return nil
}

// SanitizeLine replaces line breaks, for user-provided text (names, team
// names) placed in a message.
func SanitizeLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}
