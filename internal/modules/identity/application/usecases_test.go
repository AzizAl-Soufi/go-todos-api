// Use-case tests for the identity commands and queries, on memory stores with
// real WebAuthn (a software authenticator), a fake Firebase and a
// controllable clock.
package application_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/passkeys"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// ---- fakes ----

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

// fakeFirebase stands in for Firebase Authentication: tests sign users in
// with signIn, which returns an ID token the fake then verifies.
type fakeFirebase struct {
	mu      sync.Mutex
	tokens  map[string]application.IDClaims
	minted  []string // UIDs custom tokens were minted for
	deleted []string // deleted Firebase users
}

func newFakeFirebase() *fakeFirebase {
	return &fakeFirebase{tokens: map[string]application.IDClaims{}}
}

func (f *fakeFirebase) signIn(c application.IDClaims) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.Provider == "" {
		c.Provider = "google.com"
	}
	tok := fmt.Sprintf("id-token-%d", len(f.tokens))
	f.tokens[tok] = c
	return tok
}

func (f *fakeFirebase) VerifyIDToken(_ context.Context, tok string) (application.IDClaims, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.tokens[tok]
	if !ok {
		return application.IDClaims{}, domain.ErrInvalidIDToken
	}
	return c, nil
}

func (f *fakeFirebase) CustomToken(_ context.Context, uid string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.minted = append(f.minted, uid)
	return "custom-token-for-" + uid, nil
}

func (f *fakeFirebase) DeleteUser(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, uid)
	return nil
}

type todoGateway struct {
	deleted   []string
	deleteErr error
}

func (g *todoGateway) ListForUser(context.Context, string) ([]application.TodoSummary, error) {
	return []application.TodoSummary{}, nil
}

func (g *todoGateway) DeleteAllForUser(_ context.Context, userID string) error {
	g.deleted = append(g.deleted, userID)
	return g.deleteErr
}

// auditLog keeps the recorded security events.
type auditLog struct {
	mu      sync.Mutex
	entries []application.AuditEntry
}

func (a *auditLog) Record(_ context.Context, e application.AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, e)
	return nil
}

func (a *auditLog) DeleteUserLog(context.Context, string) error { return nil }

// last returns the most recent entry with action, or false.
func (a *auditLog) last(action string) (application.AuditEntry, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].Action == action {
			return a.entries[i], true
		}
	}
	return application.AuditEntry{}, false
}

// recordingTx counts units of work and runs them in place. The counter is
// atomic because some tests run use cases concurrently.
type recordingTx struct{ calls atomic.Int32 }

func (r *recordingTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	r.calls.Add(1)
	return fn(ctx)
}

type fixture struct {
	cmd      command.Handlers
	qry      query.Handlers
	clock    *clock
	firebase *fakeFirebase
	todos    *todoGateway
	tx       *recordingTx
	users    *memstore.Users
	passkeys *memstore.Passkeys
	audit    *auditLog
}

// newFixture builds the module; withAdmin false runs it without a Firebase
// service account.
func newFixture(t *testing.T, withAdmin ...bool) *fixture {
	t.Helper()
	f := &fixture{
		clock:    &clock{now: time.Now()},
		firebase: newFakeFirebase(),
		todos:    &todoGateway{},
		tx:       &recordingTx{},
		users:    memstore.NewUsers(),
		passkeys: memstore.NewPasskeys(),
		audit:    &auditLog{},
	}
	rpConfig, err := passkeys.ConfigFromBaseURL(passkeyOrigin, "Todos")
	if err != nil {
		t.Fatal(err)
	}
	rp, err := passkeys.New(rpConfig)
	if err != nil {
		t.Fatal(err)
	}
	var admin application.FirebaseAdmin = f.firebase
	if len(withAdmin) > 0 && !withAdmin[0] {
		admin = nil
	}
	f.cmd = command.New(command.Deps{
		Users:        f.users,
		Passkeys:     f.passkeys,
		Challenges:   memstore.NewPasskeyChallenges(),
		RelyingParty: rp,
		IDTokens:     f.firebase,
		Firebase:     admin,
		Audit:        f.audit,
		Todos:        f.todos,
		Tx:           f.tx,
		Config:       command.DefaultConfig(),
		Now:          f.clock.Now,
	})
	f.qry = query.New(query.Deps{Users: f.users, Passkeys: f.passkeys, IDTokens: f.firebase, Todos: f.todos})
	return f
}

// claims are a Firebase sign-in that happened now.
func (f *fixture) claims(uid, email string) application.IDClaims {
	return application.IDClaims{UID: uid, Email: email, EmailVerified: true, Name: "Ann", AuthTime: f.clock.Now().Truncate(time.Second)}
}

func (f *fixture) signIn(t *testing.T, c application.IDClaims) (command.SignedIn, string) {
	t.Helper()
	tok := f.firebase.signIn(c)
	res, err := f.cmd.SignIn.Handle(bg, command.SignIn{IDToken: tok})
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	return res, tok
}

// register signs Ann in for the first time and returns her account ID.
func (f *fixture) register(t *testing.T) string {
	t.Helper()
	res, _ := f.signIn(t, f.claims("uid-ann", "ann@example.com"))
	return res.UserID
}

func (f *fixture) legacyUser(t *testing.T, email string) *domain.User {
	t.Helper()
	u := &domain.User{Name: "Old", Email: email, CreatedAt: f.clock.Now()}
	if err := f.users.Create(bg, u); err != nil {
		t.Fatal(err)
	}
	return u
}

// ---- sign-in ----

func TestSignIn_CreatesTheAccountOnce(t *testing.T) {
	f := newFixture(t)
	c := f.claims("uid-ann", " Ann@Example.com ")
	c.Name = ""
	first, tok := f.signIn(t, c)
	if !first.Created || f.tx.calls.Load() != 1 {
		t.Fatalf("first sign-in: %+v, %d transactions", first, f.tx.calls.Load())
	}
	u, _ := f.users.FindByID(bg, first.UserID)
	if u.Email != "ann@example.com" || u.Name != "ann" || u.FirebaseUID != "uid-ann" || !u.EmailVerified() {
		t.Fatalf("account = %+v", u)
	}
	if e, ok := f.audit.last("user.registered"); !ok || e.Details["provider"] != "google.com" {
		t.Fatalf("registration not recorded: %+v", e)
	}

	again, err := f.cmd.SignIn.Handle(bg, command.SignIn{IDToken: tok})
	if err != nil || again.Created || again.UserID != first.UserID {
		t.Fatalf("second sign-in = %+v, %v", again, err)
	}
	if _, ok := f.audit.last("session.login"); !ok {
		t.Fatal("sign-in not recorded")
	}

	p, err := f.qry.Authenticate.Handle(bg, query.Authenticate{IDToken: tok})
	if err != nil || p.UserID != first.UserID || p.Email != "ann@example.com" || !p.EmailVerified {
		t.Fatalf("authenticate = %+v, %v", p, err)
	}
}

func TestSignIn_SyncsEmailVerification(t *testing.T) {
	f := newFixture(t)
	c := f.claims("uid-ann", "ann@example.com")
	c.EmailVerified = false
	res, _ := f.signIn(t, c)
	if u, _ := f.users.FindByID(bg, res.UserID); u.EmailVerified() {
		t.Fatal("unverified Firebase email must not verify the account")
	}
	c.EmailVerified = true
	f.signIn(t, c)
	if u, _ := f.users.FindByID(bg, res.UserID); !u.EmailVerified() {
		t.Fatal("verification in Firebase not recorded")
	}
}

func TestSignIn_LinksAnAccountFromBeforeFirebase(t *testing.T) {
	f := newFixture(t)
	old := f.legacyUser(t, "old@example.com")

	unverified := f.claims("uid-old", "old@example.com")
	unverified.EmailVerified = false
	tok := f.firebase.signIn(unverified)
	if _, err := f.cmd.SignIn.Handle(bg, command.SignIn{IDToken: tok}); !errors.Is(err, domain.ErrEmailNotVerified) {
		t.Fatalf("linking with an unverified email: err = %v, want ErrEmailNotVerified", err)
	}

	res, tok := f.signIn(t, f.claims("uid-old", "OLD@example.com"))
	if res.Created || res.UserID != old.ID {
		t.Fatalf("link = %+v, want the existing account %s", res, old.ID)
	}
	if u, _ := f.users.FindByID(bg, old.ID); u.FirebaseUID != "uid-old" || !u.EmailVerified() {
		t.Fatalf("linked account = %+v", u)
	}
	if e, ok := f.audit.last("account.linked"); !ok || e.UserID != old.ID {
		t.Fatalf("link not recorded: %+v", e)
	}
	if p, err := f.qry.Authenticate.Handle(bg, query.Authenticate{IDToken: tok}); err != nil || p.UserID != old.ID {
		t.Fatalf("authenticate after link = %+v, %v", p, err)
	}

	// Another Firebase user with the same email can't take the account.
	other := f.firebase.signIn(f.claims("uid-impostor", "old@example.com"))
	if _, err := f.cmd.SignIn.Handle(bg, command.SignIn{IDToken: other}); !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("second Firebase user for the email: err = %v, want ErrEmailTaken", err)
	}
}

func TestSignIn_Rejections(t *testing.T) {
	f := newFixture(t)
	cases := map[string]struct {
		token string
		want  error
	}{
		"unknown token":   {"forged", domain.ErrInvalidIDToken},
		"no email":        {f.firebase.signIn(f.claims("uid-phone", "")), domain.ErrNoEmailOnIDToken},
		"malformed email": {f.firebase.signIn(f.claims("uid-x", "not an email")), domain.ErrInvalidEmail},
	}
	for name, tc := range cases {
		if _, err := f.cmd.SignIn.Handle(bg, command.SignIn{IDToken: tc.token}); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

func TestSignIn_ConcurrentFirstSignInsMakeOneAccount(t *testing.T) {
	f := newFixture(t)
	tok := f.firebase.signIn(f.claims("uid-ann", "ann@example.com"))
	const n = 8
	ids := make(chan string, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			res, err := f.cmd.SignIn.Handle(bg, command.SignIn{IDToken: tok})
			if err != nil {
				t.Error(err)
			}
			ids <- res.UserID
		})
	}
	wg.Wait()
	close(ids)
	first := <-ids
	for id := range ids {
		if id != first {
			t.Fatalf("concurrent sign-ins made accounts %s and %s", first, id)
		}
	}
}

// ---- authentication ----

func TestAuthenticate_RequiresAnAccount(t *testing.T) {
	f := newFixture(t)
	tok := f.firebase.signIn(f.claims("uid-new", "new@example.com"))
	if _, err := f.qry.Authenticate.Handle(bg, query.Authenticate{IDToken: tok}); !errors.Is(err, domain.ErrAccountNotRegistered) {
		t.Fatalf("err = %v, want ErrAccountNotRegistered", err)
	}
	if _, err := f.qry.Authenticate.Handle(bg, query.Authenticate{IDToken: "forged"}); !errors.Is(err, domain.ErrInvalidIDToken) {
		t.Fatalf("forged token: err = %v", err)
	}
}

func TestLogoutAll_RevokesEarlierSignIns(t *testing.T) {
	f := newFixture(t)
	res, before := f.signIn(t, f.claims("uid-ann", "ann@example.com"))

	f.clock.Advance(time.Minute)
	if err := f.cmd.LogoutAll.Handle(bg, command.LogoutAll{UserID: res.UserID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.qry.Authenticate.Handle(bg, query.Authenticate{IDToken: before}); !errors.Is(err, domain.ErrTokenRevoked) {
		t.Fatalf("token of an earlier sign-in: err = %v, want ErrTokenRevoked", err)
	}
	if _, err := f.cmd.SignIn.Handle(bg, command.SignIn{IDToken: before}); !errors.Is(err, domain.ErrTokenRevoked) {
		t.Fatalf("signing in with a revoked token: err = %v, want ErrTokenRevoked", err)
	}

	f.clock.Advance(time.Second)
	_, after := f.signIn(t, f.claims("uid-ann", "ann@example.com"))
	if _, err := f.qry.Authenticate.Handle(bg, query.Authenticate{IDToken: after}); err != nil {
		t.Fatalf("a new sign-in must work: %v", err)
	}
	if _, ok := f.audit.last("session.logout_all"); !ok {
		t.Fatal("logout-all not recorded")
	}
}

// ---- account deletion ----

func TestDeleteAccount_RunsInOneTransactionAcrossModules(t *testing.T) {
	f := newFixture(t)
	id := f.register(t)
	f.tx.calls.Store(0)

	if err := f.cmd.DeleteAccount.Handle(bg, command.DeleteAccount{UserID: id}); err != nil {
		t.Fatal(err)
	}
	if n := f.tx.calls.Load(); n != 1 {
		t.Fatalf("delete used %d transactions, want 1", n)
	}
	if len(f.todos.deleted) != 1 || f.todos.deleted[0] != id {
		t.Fatalf("todos module not asked to delete the user's todos: %v", f.todos.deleted)
	}
	if _, err := f.users.FindByID(bg, id); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("user still present: %v", err)
	}
	if len(f.firebase.deleted) != 1 || f.firebase.deleted[0] != "uid-ann" {
		t.Fatalf("Firebase user not deleted: %v", f.firebase.deleted)
	}
}

func TestDeleteAccount_WithoutServiceAccountKeepsFirebaseUser(t *testing.T) {
	f := newFixture(t, false)
	id := f.register(t)
	if err := f.cmd.DeleteAccount.Handle(bg, command.DeleteAccount{UserID: id}); err != nil {
		t.Fatal(err)
	}
	if len(f.firebase.deleted) != 0 {
		t.Fatalf("deleted Firebase users without a service account: %v", f.firebase.deleted)
	}
}

func TestDeleteAccount_StopsWhenTodosCannotBeDeleted(t *testing.T) {
	f := newFixture(t)
	id := f.register(t)
	f.todos.deleteErr = errors.New("todos store down")

	if err := f.cmd.DeleteAccount.Handle(bg, command.DeleteAccount{UserID: id}); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := f.users.FindByID(bg, id); err != nil {
		t.Fatalf("user must survive a failed deletion: %v", err)
	}
	if len(f.firebase.deleted) != 0 {
		t.Fatal("Firebase user deleted although the account deletion failed")
	}
}
