package passkeys_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/passkeys"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/passkeys/passkeytest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

const origin = "https://app.example.test"

func newRP(t *testing.T) *passkeys.RelyingParty {
	t.Helper()
	cfg, err := passkeys.ConfigFromBaseURL(origin, "Todos")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Timeout = time.Minute
	rp, err := passkeys.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return rp
}

var ann = &domain.User{ID: "672c6170a1b2c3d4e5f60718", Name: "Ann", Email: "ann@example.com"}

// register runs a registration ceremony and returns the stored passkey and
// the authenticator's credential.
func register(t *testing.T, rp *passkeys.RelyingParty, a *passkeytest.Authenticator, existing ...*domain.Passkey) (*domain.Passkey, *passkeytest.Credential) {
	t.Helper()
	opts, state, err := rp.BeginRegistration(ann, existing)
	if err != nil {
		t.Fatal(err)
	}
	resp, cred := a.Create(t, opts)
	id, record, err := rp.FinishRegistration(ann, existing, state, resp)
	if err != nil {
		t.Fatalf("finish registration: %v", err)
	}
	if !bytes.Equal(id, cred.ID) {
		t.Fatalf("credential id = %x, want %x", id, cred.ID)
	}
	return &domain.Passkey{ID: "p1", UserID: ann.ID, CredentialID: id, Credential: record}, cred
}

func owner(passkeys ...*domain.Passkey) func(string) (*domain.User, []*domain.Passkey, error) {
	return func(userID string) (*domain.User, []*domain.Passkey, error) {
		if userID != ann.ID {
			return nil, nil, apperrors.ErrNotFound
		}
		return ann, passkeys, nil
	}
}

func TestRegisterAndLogin(t *testing.T) {
	rp, a := newRP(t), passkeytest.New(origin)
	opts, _, err := rp.BeginRegistration(ann, nil)
	if err != nil {
		t.Fatal(err)
	}
	var shape struct {
		PublicKey struct {
			RP struct {
				ID string `json:"id"`
			} `json:"rp"`
			AuthenticatorSelection struct {
				ResidentKey      string `json:"residentKey"`
				UserVerification string `json:"userVerification"`
			} `json:"authenticatorSelection"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(opts, &shape); err != nil {
		t.Fatal(err)
	}
	if sel := shape.PublicKey.AuthenticatorSelection; shape.PublicKey.RP.ID != "app.example.test" || sel.ResidentKey != "required" || sel.UserVerification != "required" {
		t.Fatalf("creation options: %s", opts)
	}

	pk, cred := register(t, rp, a)
	cred.SignCount = 1

	opts, state, err := rp.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	user, use, err := rp.FinishLogin(state, a.Get(t, opts, cred), owner(pk))
	if err != nil {
		t.Fatalf("finish login: %v", err)
	}
	if user != ann || !bytes.Equal(use.CredentialID, pk.CredentialID) || use.CloneWarning || bytes.Equal(use.Credential, pk.Credential) {
		t.Fatalf("login: user %v, use %+v", user, use)
	}

	// The updated record carries the counter: replaying an older one warns.
	pk.Credential = use.Credential
	cred.SignCount = 1
	opts, state, _ = rp.BeginLogin()
	if _, use, err = rp.FinishLogin(state, a.Get(t, opts, cred), owner(pk)); err != nil || !use.CloneWarning {
		t.Fatalf("counter went backwards: err %v, clone warning %v", err, use.CloneWarning)
	}
}

func TestRegistrationRejections(t *testing.T) {
	rp := newRP(t)
	existing, _ := register(t, rp, passkeytest.New(origin))

	opts, state, err := rp.BeginRegistration(ann, []*domain.Passkey{existing})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(opts, []byte("excludeCredentials")) {
		t.Fatalf("existing passkeys must be excluded: %s", opts)
	}

	cases := map[string]func() error{
		"wrong origin": func() error {
			resp, _ := passkeytest.New("https://evil.test").Create(t, opts)
			_, _, err := rp.FinishRegistration(ann, nil, state, resp)
			return err
		},
		"garbage": func() error {
			_, _, err := rp.FinishRegistration(ann, nil, state, []byte(`{"id":"x"}`))
			return err
		},
		"another account's challenge": func() error {
			resp, _ := passkeytest.New(origin).Create(t, opts)
			bob := &domain.User{ID: "672c6170a1b2c3d4e5f60719", Name: "Bob", Email: "bob@example.com"}
			_, _, err := rp.FinishRegistration(bob, nil, state, resp)
			return err
		},
	}
	for name, run := range cases {
		if err := run(); !errors.Is(err, domain.ErrPasskeyRejected) {
			t.Errorf("%s: err = %v, want ErrPasskeyRejected", name, err)
		}
	}
}

func TestLoginRejections(t *testing.T) {
	rp, a := newRP(t), passkeytest.New(origin)
	pk, cred := register(t, rp, a)
	other, otherCred := register(t, rp, a)

	cases := map[string]struct {
		run      func(opts []byte, state []byte) (*domain.User, error)
		wantUser bool
	}{
		"wrong origin": {run: func(opts, state []byte) (*domain.User, error) {
			u, _, err := rp.FinishLogin(state, passkeytest.New("https://evil.test").Get(t, opts, cred), owner(pk))
			return u, err
		}, wantUser: true},
		"passkey not on the account": {run: func(opts, state []byte) (*domain.User, error) {
			u, _, err := rp.FinishLogin(state, a.Get(t, opts, otherCred), owner(pk))
			return u, err
		}, wantUser: true},
		"unknown account": {run: func(opts, state []byte) (*domain.User, error) {
			c := *cred
			c.UserHandle = []byte("672c6170a1b2c3d4e5f60799")
			u, _, err := rp.FinishLogin(state, a.Get(t, opts, &c), owner(pk, other))
			return u, err
		}},
		"garbage": {run: func(_, state []byte) (*domain.User, error) {
			u, _, err := rp.FinishLogin(state, []byte(`{}`), owner(pk))
			return u, err
		}},
	}
	for name, tc := range cases {
		opts, state, err := rp.BeginLogin()
		if err != nil {
			t.Fatal(err)
		}
		user, err := tc.run(opts, state)
		if !errors.Is(err, domain.ErrInvalidPasskeyCredential) {
			t.Errorf("%s: err = %v, want ErrInvalidPasskeyCredential", name, err)
		}
		if (user != nil) != tc.wantUser {
			t.Errorf("%s: user = %v, want found %v", name, user, tc.wantUser)
		}
	}

	// A challenge answers one ceremony only.
	opts, _, _ := rp.BeginLogin()
	_, state, _ := rp.BeginLogin()
	if _, _, err := rp.FinishLogin(state, a.Get(t, opts, cred), owner(pk)); !errors.Is(err, domain.ErrInvalidPasskeyCredential) {
		t.Fatalf("answer to another challenge: err = %v", err)
	}
}

func TestConfigFromBaseURL(t *testing.T) {
	cfg, err := passkeys.ConfigFromBaseURL("http://localhost:8000/api", "X")
	if err != nil || cfg.RPID != "localhost" || len(cfg.Origins) != 1 || cfg.Origins[0] != "http://localhost:8000" {
		t.Fatalf("config = %+v, %v", cfg, err)
	}
	if _, err := passkeys.ConfigFromBaseURL("not a url", "X"); err == nil {
		t.Fatal("want an error for a URL without a host")
	}
}
