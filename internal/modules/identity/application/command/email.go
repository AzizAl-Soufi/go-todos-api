package command

import (
	"context"
	"log/slog"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// mailTimeout bounds emails sent after the command returns (password reset).
const mailTimeout = 30 * time.Second

// VerifyEmail redeems a single-use verification token.
type VerifyEmail struct {
	Token string
}

type VerifyEmailHandler struct{ *base }

func (h VerifyEmailHandler) Handle(ctx context.Context, cmd VerifyEmail) error {
	v, err := h.consumeToken(ctx, h.Verifications, cmd.Token, domain.ErrInvalidVerificationToken)
	if err != nil {
		return err
	}
	err = h.Users.MarkEmailVerified(ctx, v.UserID, h.Now())
	return apperrors.NotFoundAs(err, domain.ErrInvalidVerificationToken)
}

// ResendVerification emails a fresh link, invalidating the previous one.
type ResendVerification struct {
	UserID string
}

type ResendVerificationHandler struct{ *base }

func (h ResendVerificationHandler) Handle(ctx context.Context, cmd ResendVerification) error {
	user, err := h.Users.FindByID(ctx, cmd.UserID)
	if err != nil {
		return err
	}
	if user.EmailVerified() {
		return domain.ErrAlreadyVerified
	}
	link, err := h.issueToken(ctx, h.Verifications, user.ID, h.Config.VerificationTTL, h.Config.VerificationURL)
	if err != nil {
		return err
	}
	h.sendVerification(ctx, user, link)
	return nil
}

// RequestPasswordReset emails a single-use reset link if an account with
// this email exists. It answers the same way, in about the same time, whether
// or not it does: the email is sent after returning, so SMTP latency can't
// reveal which addresses have accounts.
type RequestPasswordReset struct {
	Email string
}

type RequestPasswordResetHandler struct{ *base }

func (h RequestPasswordResetHandler) Handle(ctx context.Context, cmd RequestPasswordReset) error {
	user, err := h.findByEmail(ctx, cmd.Email)
	if err != nil || user == nil {
		return err
	}
	link, err := h.issueToken(ctx, h.PasswordResets, user.ID, h.Config.PasswordResetTTL, h.Config.PasswordResetURL)
	if err != nil {
		return err
	}

	ctx = context.WithoutCancel(ctx)
	go func() {
		ctx, cancel := context.WithTimeout(ctx, mailTimeout)
		defer cancel()
		if err := h.Mailer.SendPasswordResetEmail(ctx, user.Email, user.Name, link); err != nil {
			slog.Error("failed to send password reset email", "user_id", user.ID, "error", err)
		}
	}()
	return nil
}

// ResetPassword sets a new password with a token from RequestPasswordReset.
// Every session and access token of the user is revoked. The email address is
// marked verified, since the user just proved they receive mail there. This
// is also how accounts created before password auth get a password.
type ResetPassword struct {
	Token, Password string
}

type ResetPasswordHandler struct{ *base }

func (h ResetPasswordHandler) Handle(ctx context.Context, cmd ResetPassword) error {
	if cmd.Token == "" {
		return domain.ErrInvalidResetToken
	}
	// Hash (and validate) first: a rejected password must not use up the token.
	hash, err := h.hashPassword(cmd.Password)
	if err != nil {
		return err
	}
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		reset, err := h.consumeToken(ctx, h.PasswordResets, cmd.Token, domain.ErrInvalidResetToken)
		if err != nil {
			return err
		}
		if err := h.Users.SetPasswordHash(ctx, reset.UserID, hash); err != nil {
			return apperrors.NotFoundAs(err, domain.ErrInvalidResetToken)
		}
		if err := h.revokeEverything(ctx, reset.UserID); err != nil {
			return err
		}
		if err := h.Users.MarkEmailVerified(ctx, reset.UserID, h.Now()); err != nil {
			return err
		}
		return h.Verifications.DeleteForUser(ctx, reset.UserID)
	})
}

// ChangePassword replaces the password of a logged-in user after checking the
// current one. Every other session and access token is revoked; the caller
// gets a fresh token pair so it stays logged in. Accounts without a password
// (created before password auth) must use the reset flow instead.
type ChangePassword struct {
	UserID, CurrentPassword, NewPassword string
}

type ChangePasswordHandler struct{ *base }

func (h ChangePasswordHandler) Handle(ctx context.Context, cmd ChangePassword) (Tokens, error) {
	user, err := h.Users.FindByID(ctx, cmd.UserID)
	if err != nil {
		return Tokens{}, err
	}
	if err := h.checkPassword(user, cmd.CurrentPassword, domain.ErrIncorrectPassword); err != nil {
		return Tokens{}, err
	}
	hash, err := h.hashPassword(cmd.NewPassword)
	if err != nil {
		return Tokens{}, err
	}

	var tokens Tokens
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Users.SetPasswordHash(ctx, user.ID, hash); err != nil {
			return err
		}
		if err := h.revokeEverything(ctx, user.ID); err != nil {
			return err
		}
		// Reload for the bumped token version the new access token must carry.
		updated, err := h.Users.FindByID(ctx, user.ID)
		if err != nil {
			return err
		}
		tokens, err = h.startSession(ctx, updated, "")
		return err
	})
	return tokens, err
}
