package command

import (
	"context"
	"errors"
	"log/slog"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/opaque"
)

// Register creates an account, starts a session and emails a verification
// link. The user, session and pending verification are stored atomically.
type Register struct {
	Name, Email, Password string
}

type RegisterHandler struct{ *base }

func (h RegisterHandler) Handle(ctx context.Context, cmd Register) (Credentials, error) {
	// Cheap checks first; bcrypt is deliberately slow.
	if err := domain.ValidatePassword(cmd.Password); err != nil {
		return Credentials{}, err
	}
	user, err := domain.NewUser(cmd.Name, cmd.Email, "", h.Now())
	if err != nil {
		return Credentials{}, err
	}
	if user.PasswordHash, err = h.hashPassword(cmd.Password); err != nil {
		return Credentials{}, err
	}

	var tokens Tokens
	var link string
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Users.Create(ctx, user); err != nil {
			return err
		}
		if tokens, err = h.startSession(ctx, user, ""); err != nil {
			return err
		}
		link, err = h.issueToken(ctx, h.Verifications, user.ID, h.Config.VerificationTTL, h.Config.VerificationURL)
		if err != nil {
			return err
		}
		return h.record(ctx, user.ID, "user.registered", nil)
	})
	if err != nil {
		return Credentials{}, err
	}
	h.sendVerification(ctx, user, link)
	return Credentials{UserID: user.ID, Tokens: tokens}, nil
}

// Login checks the password and starts a new session. Unknown emails, legacy
// accounts without a password and wrong passwords all fail the same way and
// take about the same time.
type Login struct {
	Email, Password string
}

type LoginHandler struct{ *base }

func (h LoginHandler) Handle(ctx context.Context, cmd Login) (Credentials, error) {
	user, err := h.findByEmail(ctx, cmd.Email)
	if err != nil {
		return Credentials{}, err
	}
	if err := h.checkPassword(user, cmd.Password, domain.ErrInvalidCredentials); err != nil {
		if user != nil { // the account owner should see attempts on it
			if err := h.Audit.Record(ctx, application.AuditEntry{UserID: user.ID, Action: "session.login_failed"}); err != nil {
				return Credentials{}, err
			}
		}
		return Credentials{}, err
	}
	var tokens Tokens
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if tokens, err = h.startSession(ctx, user, ""); err != nil {
			return err
		}
		return h.record(ctx, user.ID, "session.login", nil)
	})
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{UserID: user.ID, Tokens: tokens}, nil
}

// Refresh rotates a refresh token: the presented session is revoked and a new
// one in the same family is issued. Reusing a revoked token revokes the family.
type Refresh struct {
	RefreshToken string
}

type RefreshHandler struct{ *base }

func (h RefreshHandler) Handle(ctx context.Context, cmd Refresh) (Tokens, error) {
	sess, err := h.Sessions.FindByTokenHash(ctx, opaque.Hash(cmd.RefreshToken))
	if err != nil {
		return Tokens{}, apperrors.NotFoundAs(err, domain.ErrInvalidRefreshToken)
	}
	if sess.Revoked() {
		return Tokens{}, h.revokeFamilyAfterReuse(ctx, sess)
	}
	if sess.Expired(h.Now()) {
		return Tokens{}, domain.ErrInvalidRefreshToken
	}
	user, err := h.Users.FindByID(ctx, sess.UserID)
	if err != nil {
		return Tokens{}, apperrors.NotFoundAs(err, domain.ErrInvalidRefreshToken)
	}

	var tokens Tokens
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		revoked, err := h.Sessions.Revoke(ctx, sess.ID, h.Now())
		if err != nil {
			return err
		}
		if !revoked { // a concurrent request already rotated this token
			return domain.ErrRefreshTokenReused
		}
		tokens, err = h.startSession(ctx, user, sess.FamilyID)
		return err
	})
	if errors.Is(err, domain.ErrRefreshTokenReused) {
		return Tokens{}, h.revokeFamilyAfterReuse(ctx, sess)
	}
	return tokens, err
}

func (h RefreshHandler) revokeFamilyAfterReuse(ctx context.Context, sess *domain.Session) error {
	slog.Warn("refresh token reuse detected; revoking session family", "user_id", sess.UserID, "family_id", sess.FamilyID)
	err := h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Sessions.RevokeFamily(ctx, sess.FamilyID, h.Now()); err != nil {
			return err
		}
		return h.Audit.Record(ctx, application.AuditEntry{UserID: sess.UserID, Action: "session.refresh_reuse_detected"})
	})
	if err != nil {
		return err
	}
	return domain.ErrRefreshTokenReused
}

// Logout ends the login the refresh token belongs to. Unknown tokens are
// ignored so logout is idempotent.
type Logout struct {
	RefreshToken string
}

type LogoutHandler struct{ *base }

func (h LogoutHandler) Handle(ctx context.Context, cmd Logout) error {
	sess, err := h.Sessions.FindByTokenHash(ctx, opaque.Hash(cmd.RefreshToken))
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Sessions.RevokeFamily(ctx, sess.FamilyID, h.Now()); err != nil {
			return err
		}
		return h.record(ctx, sess.UserID, "session.logout", nil)
	})
}

// LogoutAll revokes every session and, by bumping the token version, every
// access token already issued to the user.
type LogoutAll struct {
	UserID string
}

type LogoutAllHandler struct{ *base }

func (h LogoutAllHandler) Handle(ctx context.Context, cmd LogoutAll) error {
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Users.IncrementTokenVersion(ctx, cmd.UserID); err != nil {
			return err
		}
		if err := h.Sessions.RevokeAllForUser(ctx, cmd.UserID, h.Now()); err != nil {
			return err
		}
		return h.record(ctx, cmd.UserID, "session.logout_all", nil)
	})
}

// DeleteAccount removes the user and everything they own in one transaction
// (where the database supports it), across modules: the user leaves their
// teams (teams they own alone are deleted; owning a team with other members
// blocks the deletion), and their personal todos and security log go too.
// Team records they created stay with the team.
type DeleteAccount struct {
	UserID string
}

type DeleteAccountHandler struct{ *base }

func (h DeleteAccountHandler) Handle(ctx context.Context, cmd DeleteAccount) error {
	id := cmd.UserID
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		for _, step := range []func() error{
			func() error { return h.Teams.RemoveUser(ctx, id) },
			func() error { return h.Todos.DeleteAllForUser(ctx, id) },
			func() error { return h.Sessions.DeleteAllForUser(ctx, id) },
			func() error { return h.Verifications.DeleteForUser(ctx, id) },
			func() error { return h.PasswordResets.DeleteForUser(ctx, id) },
			func() error { return h.Passkeys.DeleteAllForUser(ctx, id) },
			func() error { return h.Challenges.DeleteForUser(ctx, id) },
			func() error { return h.Audit.DeleteUserLog(ctx, id) },
			func() error { return h.Users.Delete(ctx, id) },
		} {
			if err := step(); err != nil {
				return err
			}
		}
		return nil
	})
}
