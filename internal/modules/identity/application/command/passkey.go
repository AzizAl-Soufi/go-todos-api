package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/opaque"
)

// PasskeyOptions starts a WebAuthn ceremony: the options for the client's
// navigator.credentials call, and the single-use token that ties the answer
// to this challenge.
type PasskeyOptions struct {
	Token   string
	Options json.RawMessage
}

// issueChallenge stores a pending ceremony for userID ("" for a sign-in) and
// returns the token that redeems it.
func (b *base) issueChallenge(ctx context.Context, userID string, state []byte) (string, error) {
	now := b.Now()
	token, hash, err := opaque.New()
	if err != nil {
		return "", err
	}
	c := &domain.PasskeyChallenge{
		TokenHash: hash, UserID: userID, State: state,
		ExpiresAt: now.Add(b.Config.PasskeyChallengeTTL), CreatedAt: now,
	}
	return token, b.Challenges.Save(ctx, c)
}

// consumeChallenge redeems a challenge token. It is used up even when the
// answer then fails to verify, so every attempt needs a fresh challenge.
func (b *base) consumeChallenge(ctx context.Context, token string) (*domain.PasskeyChallenge, error) {
	if token == "" {
		return nil, domain.ErrInvalidPasskeyChallenge
	}
	c, err := b.Challenges.Consume(ctx, opaque.Hash(token))
	if err != nil {
		return nil, apperrors.NotFoundAs(err, domain.ErrInvalidPasskeyChallenge)
	}
	if c.Expired(b.Now()) {
		return nil, domain.ErrInvalidPasskeyChallenge
	}
	return c, nil
}

// passkeysOf loads the user and their passkeys, failing once they have as
// many as an account may have. Two registrations finishing at the same time
// can still both pass; the limit is a guard against abuse, not an invariant.
func (b *base) passkeysOf(ctx context.Context, userID string) (*domain.User, []*domain.Passkey, error) {
	user, err := b.Users.FindByID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	passkeys, err := b.Passkeys.ListForUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if len(passkeys) >= domain.MaxPasskeys {
		return nil, nil, domain.ErrTooManyPasskeys
	}
	return user, passkeys, nil
}

// BeginPasskeyRegistration starts adding a passkey to the caller's account.
type BeginPasskeyRegistration struct {
	UserID string
}

type BeginPasskeyRegistrationHandler struct{ *base }

func (h BeginPasskeyRegistrationHandler) Handle(ctx context.Context, cmd BeginPasskeyRegistration) (PasskeyOptions, error) {
	user, existing, err := h.passkeysOf(ctx, cmd.UserID)
	if err != nil {
		return PasskeyOptions{}, err
	}
	options, state, err := h.RelyingParty.BeginRegistration(user, existing)
	if err != nil {
		return PasskeyOptions{}, apperrors.InternalWrap("PASSKEY_OPTIONS_FAILED", "failed to start passkey registration", err)
	}
	token, err := h.issueChallenge(ctx, user.ID, state)
	if err != nil {
		return PasskeyOptions{}, err
	}
	return PasskeyOptions{Token: token, Options: options}, nil
}

// FinishPasskeyRegistration verifies the authenticator's answer and stores
// the new passkey. The challenge must have been issued to the same account.
type FinishPasskeyRegistration struct {
	UserID   string
	Token    string
	Name     string
	Response []byte // the PublicKeyCredential from navigator.credentials.create, as JSON
}

type FinishPasskeyRegistrationHandler struct{ *base }

// Handle returns the new passkey's ID.
func (h FinishPasskeyRegistrationHandler) Handle(ctx context.Context, cmd FinishPasskeyRegistration) (string, error) {
	name, err := domain.NormalizePasskeyName(cmd.Name)
	if err != nil {
		return "", err
	}
	c, err := h.consumeChallenge(ctx, cmd.Token)
	if err != nil {
		return "", err
	}
	if !c.ForRegistrationBy(cmd.UserID) {
		return "", domain.ErrInvalidPasskeyChallenge
	}
	user, existing, err := h.passkeysOf(ctx, cmd.UserID)
	if err != nil {
		return "", err
	}
	credentialID, credential, err := h.RelyingParty.FinishRegistration(user, existing, c.State, cmd.Response)
	if err != nil {
		return "", err
	}
	passkey, err := domain.NewPasskey(user.ID, name, credentialID, credential, h.Now())
	if err != nil {
		return "", err
	}
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Passkeys.Create(ctx, passkey); err != nil {
			return err
		}
		return h.record(ctx, user.ID, "passkey.added", map[string]string{"passkeyId": passkey.ID, "name": passkey.Name})
	})
	if err != nil {
		return "", err
	}
	return passkey.ID, nil
}

// BeginPasskeyLogin starts a sign-in with a passkey. No email is needed: the
// passkey the user picks names the account.
type BeginPasskeyLogin struct{}

type BeginPasskeyLoginHandler struct{ *base }

func (h BeginPasskeyLoginHandler) Handle(ctx context.Context, _ BeginPasskeyLogin) (PasskeyOptions, error) {
	if h.Firebase == nil {
		return PasskeyOptions{}, domain.ErrPasskeyLoginOff
	}
	options, state, err := h.RelyingParty.BeginLogin()
	if err != nil {
		return PasskeyOptions{}, apperrors.InternalWrap("PASSKEY_OPTIONS_FAILED", "failed to start passkey sign-in", err)
	}
	token, err := h.issueChallenge(ctx, "", state)
	if err != nil {
		return PasskeyOptions{}, err
	}
	return PasskeyOptions{Token: token, Options: options}, nil
}

// FinishPasskeyLogin verifies the authenticator's signature and returns a
// Firebase custom token for the account; the client exchanges it with
// signInWithCustomToken for a regular Firebase sign-in. Failures on an
// existing account go to its security log. A sign counter that went
// backwards (a possibly cloned authenticator) fails the sign-in.
type FinishPasskeyLogin struct {
	Token    string
	Response []byte // the PublicKeyCredential from navigator.credentials.get, as JSON
}

// PasskeySignIn is a verified passkey sign-in.
type PasskeySignIn struct {
	UserID      string
	CustomToken string // for Firebase signInWithCustomToken
}

type FinishPasskeyLoginHandler struct{ *base }

func (h FinishPasskeyLoginHandler) Handle(ctx context.Context, cmd FinishPasskeyLogin) (PasskeySignIn, error) {
	if h.Firebase == nil {
		return PasskeySignIn{}, domain.ErrPasskeyLoginOff
	}
	c, err := h.consumeChallenge(ctx, cmd.Token)
	if err != nil {
		return PasskeySignIn{}, err
	}
	if !c.ForLogin() {
		return PasskeySignIn{}, domain.ErrInvalidPasskeyChallenge
	}

	var passkeys []*domain.Passkey
	var lookupErr error // a storage failure, as opposed to an unknown account
	owner := func(userID string) (*domain.User, []*domain.Passkey, error) {
		user, err := h.Users.FindByID(ctx, userID)
		if err == nil {
			passkeys, err = h.Passkeys.ListForUser(ctx, userID)
		}
		if err != nil && !errors.Is(err, apperrors.ErrNotFound) && !errors.Is(err, apperrors.ErrInvalidID) {
			lookupErr = err
		}
		return user, passkeys, err
	}
	user, use, err := h.RelyingParty.FinishLogin(c.State, cmd.Response, owner)
	if lookupErr != nil {
		return PasskeySignIn{}, lookupErr
	}
	if err == nil && use.CloneWarning {
		slog.Warn("passkey sign counter went backwards; refusing sign-in", "user_id", user.ID)
		err = domain.ErrInvalidPasskeyCredential
	}
	var passkey *domain.Passkey
	if err == nil {
		if passkey = findPasskey(passkeys, use.CredentialID); passkey == nil {
			err = domain.ErrInvalidPasskeyCredential
		}
	}
	if err != nil {
		if user != nil && errors.Is(err, domain.ErrInvalidPasskeyCredential) {
			entry := application.AuditEntry{UserID: user.ID, Action: "session.login_failed", Details: map[string]string{"provider": "passkey"}}
			if err := h.Audit.Record(ctx, entry); err != nil {
				return PasskeySignIn{}, err
			}
		}
		return PasskeySignIn{}, err
	}

	if user.FirebaseUID == "" {
		return PasskeySignIn{}, domain.ErrAccountNotLinked
	}
	token, err := h.Firebase.CustomToken(ctx, user.FirebaseUID)
	if err != nil {
		return PasskeySignIn{}, apperrors.InternalWrap("CUSTOM_TOKEN_FAILED", "failed to issue a Firebase token", err)
	}
	err = h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.Passkeys.RecordUse(ctx, passkey.ID, use.Credential, h.Now()); err != nil {
			return err
		}
		return h.record(ctx, user.ID, "session.login", map[string]string{"provider": "passkey", "passkeyId": passkey.ID})
	})
	if err != nil {
		return PasskeySignIn{}, err
	}
	return PasskeySignIn{UserID: user.ID, CustomToken: token}, nil
}

func findPasskey(passkeys []*domain.Passkey, credentialID []byte) *domain.Passkey {
	for _, p := range passkeys {
		if bytes.Equal(p.CredentialID, credentialID) {
			return p
		}
	}
	return nil
}

// DeletePasskey removes one of the caller's passkeys. Sign-ins it made stay
// valid; log out everywhere to end them.
type DeletePasskey struct {
	UserID    string
	PasskeyID string
}

type DeletePasskeyHandler struct{ *base }

func (h DeletePasskeyHandler) Handle(ctx context.Context, cmd DeletePasskey) error {
	return h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		passkeys, err := h.Passkeys.ListForUser(ctx, cmd.UserID)
		if err != nil {
			return err
		}
		var passkey *domain.Passkey
		for _, p := range passkeys {
			if p.ID == cmd.PasskeyID {
				passkey = p
			}
		}
		if passkey == nil {
			return domain.ErrPasskeyNotFound
		}
		if err := h.Passkeys.Delete(ctx, cmd.UserID, passkey.ID); err != nil {
			return apperrors.NotFoundAs(err, domain.ErrPasskeyNotFound)
		}
		return h.record(ctx, cmd.UserID, "passkey.removed", map[string]string{"passkeyId": passkey.ID, "name": passkey.Name})
	})
}
