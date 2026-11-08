// Package passkeys implements the identity module's RelyingParty port with
// github.com/go-webauthn/webauthn: it builds the options for the browser's
// navigator.credentials calls and verifies what the authenticator answers.
//
// Passkeys are discoverable credentials with user verification required, so
// one passkey replaces both the email and the password. The user handle
// stored on the authenticator is the account ID. Ceremony state and
// credential records are stored as the library's JSON; nothing outside this
// package reads them.
package passkeys

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
)

// Config names the relying party. RPID is the domain passkeys are bound to
// (the site's host or a parent domain of it); Origins are the exact origins
// (scheme://host[:port]) of the web pages allowed to use them.
type Config struct {
	RPID    string
	RPName  string
	Origins []string
	// Timeout is the hint given to the browser; the server enforces its own
	// challenge TTL.
	Timeout time.Duration
}

// ConfigFromBaseURL serves the passkeys from the site at baseURL.
func ConfigFromBaseURL(baseURL, name string) (Config, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" || u.Scheme == "" {
		return Config{}, fmt.Errorf("passkeys: base URL %q has no scheme and host", baseURL)
	}
	return Config{RPID: u.Hostname(), RPName: name, Origins: []string{u.Scheme + "://" + u.Host}}, nil
}

type RelyingParty struct{ wa *webauthn.WebAuthn }

var _ application.RelyingParty = (*RelyingParty)(nil)

func New(cfg Config) (*RelyingParty, error) {
	timeout := webauthn.TimeoutConfig{Timeout: cfg.Timeout, TimeoutUVD: cfg.Timeout}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:                  cfg.RPID,
		RPDisplayName:         cfg.RPName,
		RPOrigins:             cfg.Origins,
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: protocol.ResidentKeyRequired(),
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{Login: timeout, Registration: timeout},
	})
	if err != nil {
		return nil, fmt.Errorf("passkeys: %w", err)
	}
	return &RelyingParty{wa: wa}, nil
}

// user adapts an account and its passkeys to webauthn.User.
type user struct {
	u     *domain.User
	creds []webauthn.Credential
}

func (u user) WebAuthnID() []byte                         { return []byte(u.u.ID) }
func (u user) WebAuthnName() string                       { return u.u.Email }
func (u user) WebAuthnDisplayName() string                { return u.u.Name }
func (u user) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func newUser(u *domain.User, passkeys []*domain.Passkey) (user, error) {
	creds := make([]webauthn.Credential, 0, len(passkeys))
	for _, p := range passkeys {
		var c webauthn.Credential
		if err := json.Unmarshal(p.Credential, &c); err != nil {
			return user{}, fmt.Errorf("passkeys: decode credential %s: %w", p.ID, err)
		}
		creds = append(creds, c)
	}
	return user{u: u, creds: creds}, nil
}

func encode(options, session any) (json.RawMessage, []byte, error) {
	o, err := json.Marshal(options)
	if err != nil {
		return nil, nil, err
	}
	s, err := json.Marshal(session)
	return o, s, err
}

func decodeSession(state []byte) (webauthn.SessionData, error) {
	var s webauthn.SessionData
	if err := json.Unmarshal(state, &s); err != nil {
		return s, fmt.Errorf("passkeys: decode ceremony state: %w", err)
	}
	return s, nil
}

func (rp *RelyingParty) BeginRegistration(u *domain.User, existing []*domain.Passkey) (json.RawMessage, []byte, error) {
	wu, err := newUser(u, existing)
	if err != nil {
		return nil, nil, err
	}
	// Excluding the account's passkeys stops an authenticator from
	// registering a second credential for the same account.
	creation, session, err := rp.wa.BeginRegistration(wu,
		webauthn.WithExclusions(webauthn.Credentials(wu.creds).CredentialDescriptors()),
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
	)
	if err != nil {
		return nil, nil, err
	}
	return encode(creation, session)
}

func (rp *RelyingParty) FinishRegistration(u *domain.User, existing []*domain.Passkey, state, response []byte) ([]byte, []byte, error) {
	wu, err := newUser(u, existing)
	if err != nil {
		return nil, nil, err
	}
	session, err := decodeSession(state)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return nil, nil, rejected(domain.ErrPasskeyRejected, "parse registration response", err)
	}
	cred, err := rp.wa.CreateCredential(wu, session, parsed)
	if err != nil {
		return nil, nil, rejected(domain.ErrPasskeyRejected, "verify registration response", err)
	}
	record, err := json.Marshal(cred)
	if err != nil {
		return nil, nil, err
	}
	return cred.ID, record, nil
}

func (rp *RelyingParty) BeginLogin() (json.RawMessage, []byte, error) {
	assertion, session, err := rp.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, nil, err
	}
	return encode(assertion, session)
}

func (rp *RelyingParty) FinishLogin(state, response []byte, owner application.PasskeyOwner) (*domain.User, application.PasskeyUse, error) {
	session, err := decodeSession(state)
	if err != nil {
		return nil, application.PasskeyUse{}, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return nil, application.PasskeyUse{}, rejected(domain.ErrInvalidPasskeyCredential, "parse login response", err)
	}

	var found *domain.User
	lookup := func(_, userHandle []byte) (webauthn.User, error) {
		u, passkeys, err := owner(string(userHandle))
		if err != nil {
			return nil, err
		}
		found = u
		return newUser(u, passkeys)
	}
	_, cred, err := rp.wa.ValidatePasskeyLogin(lookup, session, parsed)
	if err != nil {
		return found, application.PasskeyUse{}, rejected(domain.ErrInvalidPasskeyCredential, "verify login response", err)
	}
	record, err := json.Marshal(cred)
	if err != nil {
		return nil, application.PasskeyUse{}, err
	}
	return found, application.PasskeyUse{CredentialID: cred.ID, Credential: record, CloneWarning: cred.Authenticator.CloneWarning}, nil
}

// rejected logs why the library refused a response (its details help
// debugging a client, but are not for the caller) and returns the domain error.
func rejected(as error, step string, err error) error {
	var perr *protocol.Error
	if errors.As(err, &perr) {
		slog.Debug("passkey "+step+" failed", "type", perr.Type, "details", perr.Details, "info", perr.DevInfo)
	} else {
		slog.Debug("passkey "+step+" failed", "error", err)
	}
	return as
}
