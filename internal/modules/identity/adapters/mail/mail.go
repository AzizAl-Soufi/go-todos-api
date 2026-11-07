// Package mail holds the identity module's Mailer adapters.
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

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
)

const (
	verificationSubject  = "Verify your email address"
	passwordResetSubject = "Reset your password"
)

func verificationBody(name, link string) string {
	return fmt.Sprintf("Hi %s,\r\n\r\nPlease confirm your email address by opening this link:\r\n\r\n%s\r\n\r\nIf you did not create an account, you can ignore this email.\r\n", name, link)
}

func passwordResetBody(name, link string) string {
	return fmt.Sprintf("Hi %s,\r\n\r\nYou can choose a new password by opening this link within the next hour:\r\n\r\n%s\r\n\r\nIf you did not ask to reset your password, you can ignore this email; your password stays unchanged.\r\n", name, link)
}

// LogMailer writes emails to the log instead of sending them. For local
// development and tests only: the log contains the verification link.
type LogMailer struct{}

var _ application.Mailer = LogMailer{}

func (LogMailer) SendVerificationEmail(_ context.Context, to, _, link string) error {
	slog.Info("verification email (not sent: SMTP is not configured)", "to", to, "link", link)
	return nil
}

func (LogMailer) SendPasswordResetEmail(_ context.Context, to, _, link string) error {
	slog.Info("password reset email (not sent: SMTP is not configured)", "to", to, "link", link)
	return nil
}

// SMTPMailer sends email through an SMTP server. net/smtp upgrades to TLS via
// STARTTLS when the server offers it, and refuses to send credentials without TLS
// (except to localhost).
type SMTPMailer struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

var _ application.Mailer = SMTPMailer{}

func (m SMTPMailer) SendVerificationEmail(_ context.Context, to, name, link string) error {
	return m.send(to, verificationSubject, verificationBody(sanitizeLine(name), link))
}

func (m SMTPMailer) SendPasswordResetEmail(_ context.Context, to, name, link string) error {
	return m.send(to, passwordResetSubject, passwordResetBody(sanitizeLine(name), link))
}

func (m SMTPMailer) send(to, subject, body string) error {
	if err := rejectHeaderInjection(to, m.From); err != nil {
		return err
	}
	msg := buildMessage(m.From, to, subject, body)

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

func buildMessage(from, to, subject, body string) []byte {
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

func sanitizeLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}
