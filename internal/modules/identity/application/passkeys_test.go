package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/passkeys/passkeytest"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
)

const passkeyOrigin = "http://example.test"

var bg = context.Background()

// addPasskey runs a registration ceremony for userID.
func (f *fixture) addPasskey(t *testing.T, a *passkeytest.Authenticator, userID, name string) (string, *passkeytest.Credential) {
	t.Helper()
	opts, err := f.cmd.BeginPasskeyRegistration.Handle(bg, command.BeginPasskeyRegistration{UserID: userID})
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	resp, cred := a.Create(t, opts.Options)
	id, err := f.cmd.FinishPasskeyRegistration.Handle(bg, command.FinishPasskeyRegistration{UserID: userID, Token: opts.Token, Name: name, Response: resp})
	if err != nil {
		t.Fatalf("finish registration: %v", err)
	}
	return id, cred
}

// passkeyLogin runs a sign-in ceremony with cred, answered by a.
func (f *fixture) passkeyLogin(t *testing.T, a *passkeytest.Authenticator, cred *passkeytest.Credential) (command.PasskeySignIn, error) {
	t.Helper()
	opts, err := f.cmd.BeginPasskeyLogin.Handle(bg, command.BeginPasskeyLogin{})
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	return f.cmd.FinishPasskeyLogin.Handle(bg, command.FinishPasskeyLogin{Token: opts.Token, Response: a.Get(t, opts.Options, cred)})
}

func (f *fixture) listPasskeys(t *testing.T, userID string) []*domain.Passkey {
	t.Helper()
	ps, err := f.qry.ListPasskeys.Handle(bg, query.ListPasskeys{UserID: userID})
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func TestPasskey_RegisterAndSignIn(t *testing.T) {
	f := newFixture(t)
	user := f.register(t)
	a := passkeytest.New(passkeyOrigin)

	f.tx.calls.Store(0)
	id, cred := f.addPasskey(t, a, user, "  MacBook  ")
	if n := f.tx.calls.Load(); n != 1 {
		t.Fatalf("registration used %d transactions, want 1", n)
	}
	ps := f.listPasskeys(t, user)
	if len(ps) != 1 || ps[0].ID != id || ps[0].Name != "MacBook" || ps[0].LastUsedAt != nil {
		t.Fatalf("passkeys = %+v", ps)
	}
	if e, ok := f.audit.last("passkey.added"); !ok || e.UserID != user || e.Details["name"] != "MacBook" {
		t.Fatalf("passkey.added not recorded: %+v", e)
	}

	f.tx.calls.Store(0)
	res, err := f.passkeyLogin(t, a, cred)
	if err != nil {
		t.Fatalf("passkey login: %v", err)
	}
	// The client exchanges the custom token with Firebase for an ID token.
	if res.UserID != user || res.CustomToken != "custom-token-for-uid-ann" {
		t.Fatalf("passkey sign-in = %+v", res)
	}
	if n := f.tx.calls.Load(); n != 1 {
		t.Fatalf("sign-in used %d transactions, want 1", n)
	}
	if e, ok := f.audit.last("session.login"); !ok || e.Details["provider"] != "passkey" || e.Details["passkeyId"] != id {
		t.Fatalf("login not recorded as a passkey login: %+v", e)
	}
	if ps := f.listPasskeys(t, user); ps[0].LastUsedAt == nil || !ps[0].LastUsedAt.Equal(f.clock.Now()) {
		t.Fatalf("last used = %v, want %v", ps[0].LastUsedAt, f.clock.Now())
	}
}

func TestPasskey_ChallengesAreSingleUseAndBound(t *testing.T) {
	f := newFixture(t)
	ann := f.register(t)
	bobIn, _ := f.signIn(t, f.claims("uid-bob", "bob@example.com"))
	bob := bobIn.UserID
	a := passkeytest.New(passkeyOrigin)
	_, cred := f.addPasskey(t, a, ann, "")

	register := func(userID string, opts command.PasskeyOptions) error {
		resp, _ := a.Create(t, opts.Options)
		_, err := f.cmd.FinishPasskeyRegistration.Handle(bg, command.FinishPasskeyRegistration{UserID: userID, Token: opts.Token, Response: resp})
		return err
	}
	login := func(opts command.PasskeyOptions) error {
		_, err := f.cmd.FinishPasskeyLogin.Handle(bg, command.FinishPasskeyLogin{Token: opts.Token, Response: a.Get(t, opts.Options, cred)})
		return err
	}
	beginReg := func(userID string) command.PasskeyOptions {
		o, err := f.cmd.BeginPasskeyRegistration.Handle(bg, command.BeginPasskeyRegistration{UserID: userID})
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	beginLogin := func() command.PasskeyOptions {
		o, err := f.cmd.BeginPasskeyLogin.Handle(bg, command.BeginPasskeyLogin{})
		if err != nil {
			t.Fatal(err)
		}
		return o
	}

	used := beginLogin()
	if err := login(used); err != nil {
		t.Fatal(err)
	}
	expired := beginLogin()
	f.clock.Advance(command.DefaultConfig().PasskeyChallengeTTL)

	cases := map[string]func() error{
		"reused":                         func() error { return login(used) },
		"expired":                        func() error { return login(expired) },
		"empty token":                    func() error { return login(command.PasskeyOptions{Options: used.Options}) },
		"registration token for sign-in": func() error { return login(beginReg(ann)) },
		"sign-in token for registration": func() error { return register(ann, beginLogin()) },
		"another account's registration": func() error { return register(bob, beginReg(ann)) },
	}
	for name, run := range cases {
		if err := run(); !errors.Is(err, domain.ErrInvalidPasskeyChallenge) {
			t.Errorf("%s: err = %v, want ErrInvalidPasskeyChallenge", name, err)
		}
	}
	if ps := f.listPasskeys(t, bob); len(ps) != 0 {
		t.Fatalf("bob got a passkey: %+v", ps)
	}
}

func TestPasskey_FailedSignInIsRecorded(t *testing.T) {
	f := newFixture(t)
	user := f.register(t)
	_, cred := f.addPasskey(t, passkeytest.New(passkeyOrigin), user, "")

	// A phishing page on another origin can't use the passkey.
	if _, err := f.passkeyLogin(t, passkeytest.New("http://evil.test"), cred); !errors.Is(err, domain.ErrInvalidPasskeyCredential) {
		t.Fatalf("err = %v, want ErrInvalidPasskeyCredential", err)
	}
	if e, ok := f.audit.last("session.login_failed"); !ok || e.UserID != user || e.ActorID != "" || e.Details["provider"] != "passkey" {
		t.Fatalf("failed sign-in not recorded: %+v", e)
	}
}

func TestPasskey_ClonedAuthenticatorIsRefused(t *testing.T) {
	f := newFixture(t)
	user := f.register(t)
	a := passkeytest.New(passkeyOrigin)
	_, cred := f.addPasskey(t, a, user, "")

	cred.SignCount = 5
	if _, err := f.passkeyLogin(t, a, cred); err != nil {
		t.Fatal(err)
	}
	cred.SignCount = 3 // a copy of the key that signed fewer times
	if _, err := f.passkeyLogin(t, a, cred); !errors.Is(err, domain.ErrInvalidPasskeyCredential) {
		t.Fatalf("err = %v, want ErrInvalidPasskeyCredential", err)
	}
}

func TestPasskey_Limit(t *testing.T) {
	f := newFixture(t)
	user := f.register(t)
	a := passkeytest.New(passkeyOrigin)
	for range domain.MaxPasskeys {
		f.addPasskey(t, a, user, "")
	}
	if _, err := f.cmd.BeginPasskeyRegistration.Handle(bg, command.BeginPasskeyRegistration{UserID: user}); !errors.Is(err, domain.ErrTooManyPasskeys) {
		t.Fatalf("err = %v, want ErrTooManyPasskeys", err)
	}
}

func TestPasskey_InvalidName(t *testing.T) {
	f := newFixture(t)
	user := f.register(t)
	_, err := f.cmd.FinishPasskeyRegistration.Handle(bg, command.FinishPasskeyRegistration{
		UserID: user, Token: "whatever", Name: strings.Repeat("é", domain.MaxPasskeyNameLength+1),
	})
	if !errors.Is(err, domain.ErrPasskeyNameTooLong) {
		t.Fatalf("err = %v, want ErrPasskeyNameTooLong", err)
	}
}

func TestPasskey_Delete(t *testing.T) {
	f := newFixture(t)
	ann := f.register(t)
	bobIn, _ := f.signIn(t, f.claims("uid-bob", "bob@example.com"))
	bob := bobIn.UserID
	a := passkeytest.New(passkeyOrigin)
	id, cred := f.addPasskey(t, a, ann, "Phone")

	for name, cmd := range map[string]command.DeletePasskey{
		"another account's passkey": {UserID: bob, PasskeyID: id},
		"unknown id":                {UserID: ann, PasskeyID: "nope"},
	} {
		if err := f.cmd.DeletePasskey.Handle(bg, cmd); !errors.Is(err, domain.ErrPasskeyNotFound) {
			t.Errorf("%s: err = %v, want ErrPasskeyNotFound", name, err)
		}
	}

	if err := f.cmd.DeletePasskey.Handle(bg, command.DeletePasskey{UserID: ann, PasskeyID: id}); err != nil {
		t.Fatal(err)
	}
	if e, ok := f.audit.last("passkey.removed"); !ok || e.Details["passkeyId"] != id || e.Details["name"] != "Phone" {
		t.Fatalf("passkey.removed not recorded: %+v", e)
	}
	if _, err := f.passkeyLogin(t, a, cred); !errors.Is(err, domain.ErrInvalidPasskeyCredential) {
		t.Fatalf("sign-in with a deleted passkey: err = %v", err)
	}
}

func TestDeleteAccount_RemovesPasskeys(t *testing.T) {
	f := newFixture(t)
	user := f.register(t)
	a := passkeytest.New(passkeyOrigin)
	_, cred := f.addPasskey(t, a, user, "")

	if err := f.cmd.DeleteAccount.Handle(bg, command.DeleteAccount{UserID: user}); err != nil {
		t.Fatal(err)
	}
	if ps := f.listPasskeys(t, user); len(ps) != 0 {
		t.Fatalf("passkeys left after account deletion: %+v", ps)
	}
	if _, err := f.passkeyLogin(t, a, cred); !errors.Is(err, domain.ErrInvalidPasskeyCredential) {
		t.Fatalf("sign-in after account deletion: err = %v", err)
	}
}

func TestPasskey_SignInNeedsAServiceAccount(t *testing.T) {
	f := newFixture(t, false)
	user := f.register(t)
	f.addPasskey(t, passkeytest.New(passkeyOrigin), user, "") // adding one still works
	if _, err := f.cmd.BeginPasskeyLogin.Handle(bg, command.BeginPasskeyLogin{}); !errors.Is(err, domain.ErrPasskeyLoginOff) {
		t.Fatalf("err = %v, want ErrPasskeyLoginOff", err)
	}
	if _, err := f.cmd.FinishPasskeyLogin.Handle(bg, command.FinishPasskeyLogin{Token: "x"}); !errors.Is(err, domain.ErrPasskeyLoginOff) {
		t.Fatalf("finish: err = %v, want ErrPasskeyLoginOff", err)
	}
}

func TestPasskey_UnlinkedAccountCantSignIn(t *testing.T) {
	f := newFixture(t)
	old := f.legacyUser(t, "old@example.com")
	a := passkeytest.New(passkeyOrigin)
	_, cred := f.addPasskey(t, a, old.ID, "")
	if _, err := f.passkeyLogin(t, a, cred); !errors.Is(err, domain.ErrAccountNotLinked) {
		t.Fatalf("err = %v, want ErrAccountNotLinked", err)
	}
	if len(f.firebase.minted) != 0 {
		t.Fatalf("minted custom tokens: %v", f.firebase.minted)
	}
}
