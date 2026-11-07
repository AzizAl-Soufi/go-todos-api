package command

import (
	"context"
	"errors"
	"log/slog"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// SignIn is called by the client after every Firebase sign-in, with the
// Firebase ID token. It finds the account of that Firebase user, or else
// links the account with the same email (only when Firebase has verified the
// email, so nobody can take over an account by signing up in Firebase with
// someone else's address), or else creates one.
type SignIn struct {
	IDToken string
}

// SignedIn names the account; Created is true for a new one.
type SignedIn struct {
	UserID  string
	Created bool
}

type SignInHandler struct{ *base }

func (h SignInHandler) Handle(ctx context.Context, cmd SignIn) (SignedIn, error) {
	claims, err := h.IDTokens.VerifyIDToken(ctx, cmd.IDToken)
	if err != nil {
		return SignedIn{}, err
	}
	out, err := h.signIn(ctx, claims)
	if errors.Is(err, domain.ErrFirebaseUIDTaken) || errors.Is(err, domain.ErrEmailTaken) {
		// A concurrent first sign-in of the same Firebase user may have
		// created or linked the account in the meantime (the losing insert
		// hits the email or the UID index); this time it is found by UID.
		// A real conflict fails the same way again.
		out, err = h.signIn(ctx, claims)
	}
	return out, err
}

func (h SignInHandler) signIn(ctx context.Context, c application.IDClaims) (SignedIn, error) {
	var out SignedIn
	details := map[string]string{"provider": c.Provider}
	err := h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		user, err := h.Users.FindByFirebaseUID(ctx, c.UID)
		if err == nil {
			if user.SignInRevoked(c.AuthTime) {
				return domain.ErrTokenRevoked
			}
			if err := h.syncVerified(ctx, user, c); err != nil {
				return err
			}
			out.UserID = user.ID
			return h.record(ctx, user.ID, "session.login", details)
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}

		if c.Email == "" {
			return domain.ErrNoEmailOnIDToken
		}
		email, err := domain.NormalizeEmail(c.Email)
		if err != nil {
			return err
		}
		existing, err := h.Users.FindByEmail(ctx, email)
		switch {
		case err == nil:
			return h.link(ctx, existing, c, &out)
		case !errors.Is(err, apperrors.ErrNotFound):
			return err
		}

		user, err = domain.NewUser(c.UID, c.Name, email, c.EmailVerified, h.Now())
		if err != nil {
			return err
		}
		if err := h.Users.Create(ctx, user); err != nil {
			return err
		}
		out = SignedIn{UserID: user.ID, Created: true}
		return h.record(ctx, user.ID, "user.registered", details)
	})
	return out, err
}

// link attaches the Firebase user to an existing account with its email.
func (h SignInHandler) link(ctx context.Context, user *domain.User, c application.IDClaims, out *SignedIn) error {
	if !c.EmailVerified {
		return domain.ErrEmailNotVerified
	}
	if user.FirebaseUID != "" { // another Firebase user already owns this email here
		return domain.ErrEmailTaken
	}
	if err := h.Users.LinkFirebaseUID(ctx, user.ID, c.UID); err != nil {
		return err
	}
	if err := h.syncVerified(ctx, user, c); err != nil {
		return err
	}
	out.UserID = user.ID
	return h.record(ctx, user.ID, "account.linked", map[string]string{"provider": c.Provider})
}

// syncVerified records that Firebase verified the email.
func (h SignInHandler) syncVerified(ctx context.Context, user *domain.User, c application.IDClaims) error {
	if !c.EmailVerified || user.EmailVerified() {
		return nil
	}
	return h.Users.MarkEmailVerified(ctx, user.ID, h.Now())
}

// LogoutAll refuses, from now on, every Firebase ID token of a sign-in that
// happened before: the user has to sign in again on every device. Refreshed
// ID tokens keep their sign-in time, so they are refused too.
type LogoutAll struct {
	UserID string
}

type LogoutAllHandler struct{ *base }

func (h LogoutAllHandler) Handle(ctx context.Context, cmd LogoutAll) error {
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Users.RevokeSessions(ctx, cmd.UserID, h.Now()); err != nil {
			return err
		}
		return h.record(ctx, cmd.UserID, "session.logout_all", nil)
	})
}

// DeleteAccount removes the user and everything they own in one transaction
// (where the database supports it), across modules: the user leaves their
// teams (teams they own alone are deleted; owning a team with other members
// blocks the deletion), and their personal todos, passkeys and security log
// go too. Team records they created stay with the team. With a service
// account the Firebase user is deleted afterwards; that can't be part of the
// transaction, so a failure there is only logged.
type DeleteAccount struct {
	UserID string
}

type DeleteAccountHandler struct{ *base }

func (h DeleteAccountHandler) Handle(ctx context.Context, cmd DeleteAccount) error {
	id := cmd.UserID
	user, err := h.Users.FindByID(ctx, id)
	if err != nil {
		return err
	}
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		for _, step := range []func() error{
			func() error { return h.Teams.RemoveUser(ctx, id) },
			func() error { return h.Todos.DeleteAllForUser(ctx, id) },
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
	if err != nil {
		return err
	}
	if h.Firebase != nil && user.FirebaseUID != "" {
		if err := h.Firebase.DeleteUser(ctx, user.FirebaseUID); err != nil {
			slog.Error("account deleted but its Firebase user was not", "user_id", id, "error", err)
		}
	}
	return nil
}
