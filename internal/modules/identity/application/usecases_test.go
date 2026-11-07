// Use-case tests for the identity commands and queries, on memory stores with
// real bcrypt (minimum cost) and JWT, a fake mailer and a controllable clock.
package application_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/passkeys"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/persistence/memstore"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/adapters/security"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application/command"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application/query"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/testutil"
	"golang.org/x/crypto/bcrypt"
)

// ---- fakes ----

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

type mailer struct {
	mu     sync.Mutex
	links  []string // verification links
	resets []string // password reset links
	err    error
}

func (m *mailer) SendVerificationEmail(_ context.Context, _, _, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links = append(m.links, link)
	return m.err
}

func (m *mailer) SendPasswordResetEmail(_ context.Context, _, _, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resets = append(m.resets, link)
	return m.err
}

func (m *mailer) lastToken(t *testing.T) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.links) == 0 {
		t.Fatal("no verification email sent")
	}
	return tokenOf(t, m.links[len(m.links)-1])
}

// resetToken waits for the n-th password reset email (1-based); it is sent
// in the background after RequestPasswordReset returns.
func (m *mailer) resetToken(t *testing.T, n int) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.mu.Lock()
		if len(m.resets) >= n {
			link := m.resets[n-1]
			m.mu.Unlock()
			return tokenOf(t, link)
		}
		m.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("password reset email #%d not sent", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func tokenOf(t *testing.T, link string) string {
	t.Helper()
	_, token, ok := strings.Cut(link, "?token=")
	if !ok {
		t.Fatalf("link has no token: %s", link)
	}
	return token
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
	mailer   *mailer
	todos    *todoGateway
	tx       *recordingTx
	users    *memstore.Users
	sessions *memstore.Sessions
	resets   *memstore.OneTimeTokens
	passkeys *memstore.Passkeys
	audit    *auditLog
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		// Starts at the real time: the JWT adapter checks expiry against it.
		clock:    &clock{now: time.Now()},
		mailer:   &mailer{},
		todos:    &todoGateway{},
		tx:       &recordingTx{},
		users:    memstore.NewUsers(),
		sessions: memstore.NewSessions(),
		resets:   memstore.NewPasswordResets(),
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
	tokens := security.NewJWT(testutil.NewRSAKey(t), 15*time.Minute)
	f.cmd = command.New(command.Deps{
		Users:          f.users,
		Sessions:       f.sessions,
		Verifications:  memstore.NewVerifications(),
		PasswordResets: f.resets,
		Passkeys:       f.passkeys,
		Challenges:     memstore.NewPasskeyChallenges(),
		RelyingParty:   rp,
		Audit:          f.audit,
		Hasher:         security.NewBcryptHasher(bcrypt.MinCost),
		Tokens:         tokens,
		Mailer:         f.mailer,
		Todos:          f.todos,
		Tx:             f.tx,
		Config:         command.DefaultConfig("http://example.test"),
		Now:            f.clock.Now,
	})
	f.qry = query.New(query.Deps{Users: f.users, Passkeys: f.passkeys, Tokens: tokens, Todos: f.todos})
	return f
}

func (f *fixture) register(t *testing.T) command.Credentials {
	t.Helper()
	res, err := f.cmd.Register.Handle(context.Background(), command.Register{Name: "Ann", Email: "ann@example.com", Password: "password1"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return res
}

// ---- tests ----

func TestRegister_StoresHashNotPasswordAndUsesOneTransaction(t *testing.T) {
	f := newFixture(t)
	res := f.register(t)

	stored, _ := f.users.FindByID(context.Background(), res.UserID)
	if stored.PasswordHash == "" || stored.PasswordHash == "password1" {
		t.Fatalf("password must be stored hashed, got %q", stored.PasswordHash)
	}
	if n := f.tx.calls.Load(); n != 1 {
		t.Fatalf("register used %d transactions, want 1", n)
	}
	if len(f.mailer.links) != 1 {
		t.Fatalf("sent %d verification emails, want 1", len(f.mailer.links))
	}
}

func TestRegister_MailFailureDoesNotFailRegistration(t *testing.T) {
	f := newFixture(t)
	f.mailer.err = errors.New("smtp down")
	f.register(t)
}

func TestRefresh_ExpiredSession(t *testing.T) {
	f := newFixture(t)
	res := f.register(t)

	f.clock.Advance(7*24*time.Hour + time.Second)
	if _, err := f.cmd.Refresh.Handle(context.Background(), command.Refresh{RefreshToken: res.Tokens.RefreshToken}); !errors.Is(err, domain.ErrInvalidRefreshToken) {
		t.Fatalf("err = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestRefresh_ConcurrentUseRotatesOnlyOnce(t *testing.T) {
	f := newFixture(t)
	res := f.register(t)

	const n = 8
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = f.cmd.Refresh.Handle(context.Background(), command.Refresh{RefreshToken: res.Tokens.RefreshToken})
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case !errors.Is(err, domain.ErrRefreshTokenReused):
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d concurrent refreshes succeeded, want exactly 1", wins)
	}
}

func TestVerifyEmail_ExpiredToken(t *testing.T) {
	f := newFixture(t)
	f.register(t)
	token := f.mailer.lastToken(t)

	f.clock.Advance(24*time.Hour + time.Second)
	if err := f.cmd.VerifyEmail.Handle(context.Background(), command.VerifyEmail{Token: token}); !errors.Is(err, domain.ErrInvalidVerificationToken) {
		t.Fatalf("err = %v, want ErrInvalidVerificationToken", err)
	}
}

func TestAuthenticate_ExpiredAccessToken(t *testing.T) {
	f := newFixture(t)
	res := f.register(t)

	if _, err := f.qry.Authenticate.Handle(context.Background(), query.Authenticate{AccessToken: res.Tokens.AccessToken}); err != nil {
		t.Fatalf("fresh token rejected: %v", err)
	}
	// The JWT adapter checks expiry against the wall clock, so issue a token
	// "in the past" by moving the service clock back.
	f.clock.Advance(-time.Hour)
	old, err := f.cmd.Login.Handle(context.Background(), command.Login{Email: "ann@example.com", Password: "password1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.qry.Authenticate.Handle(context.Background(), query.Authenticate{AccessToken: old.Tokens.AccessToken}); !errors.Is(err, domain.ErrExpiredAccessToken) {
		t.Fatalf("err = %v, want ErrExpiredAccessToken", err)
	}
}

func TestLogin_LegacyAccountWithoutPassword(t *testing.T) {
	f := newFixture(t)
	legacy := &domain.User{Name: "Old", Email: "old@example.com", CreatedAt: f.clock.Now()}
	if err := f.users.Create(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := f.cmd.Login.Handle(context.Background(), command.Login{Email: "old@example.com", Password: ""}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestDeleteAccount_RunsInOneTransactionAcrossModules(t *testing.T) {
	f := newFixture(t)
	res := f.register(t)
	f.tx.calls.Store(0)

	if err := f.cmd.DeleteAccount.Handle(context.Background(), command.DeleteAccount{UserID: res.UserID}); err != nil {
		t.Fatal(err)
	}
	if n := f.tx.calls.Load(); n != 1 {
		t.Fatalf("delete used %d transactions, want 1", n)
	}
	if len(f.todos.deleted) != 1 || f.todos.deleted[0] != res.UserID {
		t.Fatalf("todos module not asked to delete the user's todos: %v", f.todos.deleted)
	}
	if _, err := f.users.FindByID(context.Background(), res.UserID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("user still present: %v", err)
	}
}

func TestDeleteAccount_StopsWhenTodosCannotBeDeleted(t *testing.T) {
	f := newFixture(t)
	res := f.register(t)
	f.todos.deleteErr = errors.New("todos store down")

	if err := f.cmd.DeleteAccount.Handle(context.Background(), command.DeleteAccount{UserID: res.UserID}); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := f.users.FindByID(context.Background(), res.UserID); err != nil {
		t.Fatalf("user must not be deleted when todos deletion fails: %v", err)
	}
}

func TestLogoutAll_BumpsTokenVersion(t *testing.T) {
	f := newFixture(t)
	res := f.register(t)

	if err := f.cmd.LogoutAll.Handle(context.Background(), command.LogoutAll{UserID: res.UserID}); err != nil {
		t.Fatal(err)
	}
	u, _ := f.users.FindByID(context.Background(), res.UserID)
	if u.TokenVersion != 1 {
		t.Fatalf("token version = %d, want 1", u.TokenVersion)
	}
	if _, err := f.qry.Authenticate.Handle(context.Background(), query.Authenticate{AccessToken: res.Tokens.AccessToken}); !errors.Is(err, domain.ErrTokenRevoked) {
		t.Fatalf("err = %v, want ErrTokenRevoked", err)
	}
}

// ---- password reset and change ----

var ctx = context.Background()

func (f *fixture) requestReset(t *testing.T, email string, n int) string {
	t.Helper()
	if err := f.cmd.RequestPasswordReset.Handle(ctx, command.RequestPasswordReset{Email: email}); err != nil {
		t.Fatalf("request reset: %v", err)
	}
	return f.mailer.resetToken(t, n)
}

func TestRequestPasswordReset_UnknownEmailLooksTheSame(t *testing.T) {
	f := newFixture(t)
	for _, email := range []string{"nobody@example.com", "not an email", ""} {
		if err := f.cmd.RequestPasswordReset.Handle(ctx, command.RequestPasswordReset{Email: email}); err != nil {
			t.Fatalf("%q: err = %v, want nil", email, err)
		}
	}
	if len(f.resets.Data) != 0 {
		t.Fatal("no reset may be stored for unknown accounts")
	}
}

func TestResetPassword(t *testing.T) {
	f := newFixture(t)
	res := f.register(t)
	token := f.requestReset(t, " ANN@example.com ", 1)

	// A rejected password must not burn the token.
	if err := f.cmd.ResetPassword.Handle(ctx, command.ResetPassword{Token: token, Password: "short"}); !errors.Is(err, domain.ErrWeakPassword) {
		t.Fatalf("weak password: err = %v", err)
	}
	if err := f.cmd.ResetPassword.Handle(ctx, command.ResetPassword{Token: token, Password: "brand new password"}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := f.cmd.ResetPassword.Handle(ctx, command.ResetPassword{Token: token, Password: "another password"}); !errors.Is(err, domain.ErrInvalidResetToken) {
		t.Fatalf("reused token: err = %v, want ErrInvalidResetToken", err)
	}

	if _, err := f.cmd.Login.Handle(ctx, command.Login{Email: "ann@example.com", Password: "password1"}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatal("the old password must stop working")
	}
	if _, err := f.cmd.Login.Handle(ctx, command.Login{Email: "ann@example.com", Password: "brand new password"}); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	if _, err := f.qry.Authenticate.Handle(ctx, query.Authenticate{AccessToken: res.Tokens.AccessToken}); !errors.Is(err, domain.ErrTokenRevoked) {
		t.Fatalf("old access token: err = %v, want ErrTokenRevoked", err)
	}
	if _, err := f.cmd.Refresh.Handle(ctx, command.Refresh{RefreshToken: res.Tokens.RefreshToken}); err == nil {
		t.Fatal("old refresh token must be revoked")
	}
	if u, _ := f.users.FindByID(ctx, res.UserID); !u.EmailVerified() {
		t.Fatal("a completed reset proves the email address")
	}
}

func TestResetPassword_InvalidTokens(t *testing.T) {
	f := newFixture(t)
	f.register(t)
	first := f.requestReset(t, "ann@example.com", 1)
	second := f.requestReset(t, "ann@example.com", 2)

	for name, token := range map[string]string{"empty": "", "unknown": "nope", "replaced by a newer request": first} {
		if err := f.cmd.ResetPassword.Handle(ctx, command.ResetPassword{Token: token, Password: "brand new password"}); !errors.Is(err, domain.ErrInvalidResetToken) {
			t.Fatalf("%s: err = %v, want ErrInvalidResetToken", name, err)
		}
	}

	f.clock.Advance(time.Hour + time.Second)
	if err := f.cmd.ResetPassword.Handle(ctx, command.ResetPassword{Token: second, Password: "brand new password"}); !errors.Is(err, domain.ErrInvalidResetToken) {
		t.Fatalf("expired: err = %v, want ErrInvalidResetToken", err)
	}
}

func TestResetPassword_GivesLegacyAccountsAPassword(t *testing.T) {
	f := newFixture(t)
	legacy := &domain.User{Name: "Old", Email: "old@example.com", CreatedAt: f.clock.Now()}
	if err := f.users.Create(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	token := f.requestReset(t, "old@example.com", 1)
	if err := f.cmd.ResetPassword.Handle(ctx, command.ResetPassword{Token: token, Password: "finally a password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.cmd.Login.Handle(ctx, command.Login{Email: "old@example.com", Password: "finally a password"}); err != nil {
		t.Fatalf("legacy login after reset: %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	f := newFixture(t)
	res := f.register(t)
	other, _ := f.cmd.Login.Handle(ctx, command.Login{Email: "ann@example.com", Password: "password1"})
	pending := f.requestReset(t, "ann@example.com", 1)

	if _, err := f.cmd.ChangePassword.Handle(ctx, command.ChangePassword{UserID: res.UserID, CurrentPassword: "wrong", NewPassword: "brand new password"}); !errors.Is(err, domain.ErrIncorrectPassword) {
		t.Fatalf("wrong current password: err = %v", err)
	}
	if _, err := f.cmd.ChangePassword.Handle(ctx, command.ChangePassword{UserID: res.UserID, CurrentPassword: "password1", NewPassword: "short"}); !errors.Is(err, domain.ErrWeakPassword) {
		t.Fatalf("weak new password: err = %v", err)
	}

	tokens, err := f.cmd.ChangePassword.Handle(ctx, command.ChangePassword{UserID: res.UserID, CurrentPassword: "password1", NewPassword: "brand new password"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.qry.Authenticate.Handle(ctx, query.Authenticate{AccessToken: tokens.AccessToken}); err != nil {
		t.Fatalf("the new access token must work: %v", err)
	}
	if _, err := f.cmd.Refresh.Handle(ctx, command.Refresh{RefreshToken: tokens.RefreshToken}); err != nil {
		t.Fatalf("the new refresh token must work: %v", err)
	}
	if _, err := f.qry.Authenticate.Handle(ctx, query.Authenticate{AccessToken: other.Tokens.AccessToken}); !errors.Is(err, domain.ErrTokenRevoked) {
		t.Fatalf("other logins' access tokens: err = %v, want ErrTokenRevoked", err)
	}
	if _, err := f.cmd.Refresh.Handle(ctx, command.Refresh{RefreshToken: other.Tokens.RefreshToken}); err == nil {
		t.Fatal("other logins' refresh tokens must be revoked")
	}
	if err := f.cmd.ResetPassword.Handle(ctx, command.ResetPassword{Token: pending, Password: "sneaky password"}); !errors.Is(err, domain.ErrInvalidResetToken) {
		t.Fatal("changing the password must cancel a pending reset link")
	}
	if _, err := f.cmd.Login.Handle(ctx, command.Login{Email: "ann@example.com", Password: "brand new password"}); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
}

func TestChangePassword_LegacyAccountMustUseReset(t *testing.T) {
	f := newFixture(t)
	legacy := &domain.User{Name: "Old", Email: "old@example.com", CreatedAt: f.clock.Now()}
	if err := f.users.Create(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := f.cmd.ChangePassword.Handle(ctx, command.ChangePassword{UserID: legacy.ID, CurrentPassword: "", NewPassword: "brand new password"}); !errors.Is(err, domain.ErrIncorrectPassword) {
		t.Fatalf("err = %v, want ErrIncorrectPassword", err)
	}
}

func TestDeleteAccount_RemovesPendingPasswordReset(t *testing.T) {
	f := newFixture(t)
	res := f.register(t)
	f.requestReset(t, "ann@example.com", 1)

	if err := f.cmd.DeleteAccount.Handle(ctx, command.DeleteAccount{UserID: res.UserID}); err != nil {
		t.Fatal(err)
	}
	if len(f.resets.Data) != 0 {
		t.Fatal("pending password reset left behind")
	}
}
