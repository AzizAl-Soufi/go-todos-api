// Package mail writes the teams module's invitation email and hands it to a
// platform mail.Sender.
package mail

import (
	"context"
	"fmt"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/teams/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/mail"
)

func invitationSubject(team string) string {
	return fmt.Sprintf("You're invited to join %s", mail.SanitizeLine(team))
}

func invitationBody(inviter, team string, role access.Role, link string) string {
	return fmt.Sprintf("Hi,\r\n\r\n%s invited you to join the team %q as %s.\r\n\r\nOpen this link within the next 7 days to accept or decline:\r\n\r\n%s\r\n\r\nYou'll need to sign in (or create an account) with this email address. If you weren't expecting this, you can ignore this email.\r\n",
		mail.SanitizeLine(inviter), mail.SanitizeLine(team), role, link)
}

// Mailer implements application.Mailer on a mail.Sender.
type Mailer struct {
	Sender mail.Sender
}

var _ application.Mailer = Mailer{}

func (m Mailer) SendInvitation(ctx context.Context, to, inviter, team string, role access.Role, link string) error {
	return m.Sender.Send(ctx, to, invitationSubject(team), invitationBody(inviter, team, role, link))
}
