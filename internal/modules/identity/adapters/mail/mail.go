// Package mail writes the identity module's emails (verification, password
// reset) and hands them to a platform mail.Sender.
package mail

import (
	"context"
	"fmt"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/mail"
)

const (
	verificationSubject  = "Verify your email address"
	passwordResetSubject = "Reset your password"
)

func verificationBody(name, link string) string {
	return fmt.Sprintf("Hi %s,\r\n\r\nPlease confirm your email address by opening this link:\r\n\r\n%s\r\n\r\nIf you did not create an account, you can ignore this email.\r\n", mail.SanitizeLine(name), link)
}

func passwordResetBody(name, link string) string {
	return fmt.Sprintf("Hi %s,\r\n\r\nYou can choose a new password by opening this link within the next hour:\r\n\r\n%s\r\n\r\nIf you did not ask to reset your password, you can ignore this email; your password stays unchanged.\r\n", mail.SanitizeLine(name), link)
}

// Mailer implements application.Mailer on a mail.Sender.
type Mailer struct {
	Sender mail.Sender
}

var _ application.Mailer = Mailer{}

func (m Mailer) SendVerificationEmail(ctx context.Context, to, name, link string) error {
	return m.Sender.Send(ctx, to, verificationSubject, verificationBody(name, link))
}

func (m Mailer) SendPasswordResetEmail(ctx context.Context, to, name, link string) error {
	return m.Sender.Send(ctx, to, passwordResetSubject, passwordResetBody(name, link))
}
