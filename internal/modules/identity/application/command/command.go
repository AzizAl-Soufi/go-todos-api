// Package command holds the identity module's write use cases. Each command
// is a plain struct, handled by its own handler. Handlers return nothing but
// what only the write can produce: the account's ID, a new passkey's ID, the
// options of a WebAuthn ceremony, or a Firebase custom token. Callers read
// everything else back through the query side.
//
// Signing in is Firebase Authentication's job (Google, email and password);
// the client then presents Firebase ID tokens. The commands here keep the
// API's account in step with the Firebase user.
package command

import (
	"context"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
)

type Config struct {
	// PasskeyChallengeTTL is how long a passkey registration or sign-in may
	// take between the options and the answer.
	PasskeyChallengeTTL time.Duration
}

func DefaultConfig() Config {
	return Config{PasskeyChallengeTTL: 5 * time.Minute}
}

type Deps struct {
	Users        application.UserRepository
	Passkeys     application.PasskeyRepository
	Challenges   application.PasskeyChallengeRepository
	RelyingParty application.RelyingParty
	IDTokens     application.IDTokenVerifier
	// Firebase is nil without a service account: passkey sign-in is then
	// disabled, and deleting an account leaves its Firebase user behind.
	Firebase application.FirebaseAdmin
	Todos    application.TodoDeleter
	Teams    application.TeamMemberships // nil: no teams to leave
	Audit    application.AuditLog        // nil records nothing
	Tx       tx.Runner
	Config   Config
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Handlers are all identity commands.
type Handlers struct {
	SignIn        SignInHandler
	LogoutAll     LogoutAllHandler
	DeleteAccount DeleteAccountHandler

	BeginPasskeyRegistration  BeginPasskeyRegistrationHandler
	FinishPasskeyRegistration FinishPasskeyRegistrationHandler
	BeginPasskeyLogin         BeginPasskeyLoginHandler
	FinishPasskeyLogin        FinishPasskeyLoginHandler
	DeletePasskey             DeletePasskeyHandler
}

func New(d Deps) Handlers {
	if d.Tx == nil {
		d.Tx = tx.None{}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Teams == nil {
		d.Teams = noTeams{}
	}
	if d.Audit == nil {
		d.Audit = noAudit{}
	}
	b := &base{Deps: d}
	return Handlers{
		SignIn: SignInHandler{b}, LogoutAll: LogoutAllHandler{b}, DeleteAccount: DeleteAccountHandler{b},
		BeginPasskeyRegistration: BeginPasskeyRegistrationHandler{b}, FinishPasskeyRegistration: FinishPasskeyRegistrationHandler{b},
		BeginPasskeyLogin: BeginPasskeyLoginHandler{b}, FinishPasskeyLogin: FinishPasskeyLoginHandler{b},
		DeletePasskey: DeletePasskeyHandler{b},
	}
}

type (
	noTeams struct{}
	noAudit struct{}
)

func (noTeams) RemoveUser(context.Context, string) error             { return nil }
func (noAudit) Record(context.Context, application.AuditEntry) error { return nil }
func (noAudit) DeleteUserLog(context.Context, string) error          { return nil }

// base is what every handler shares: the dependencies and the steps several
// commands reuse.
type base struct{ Deps }

// record writes a security event about userID, done by userID itself.
func (b *base) record(ctx context.Context, userID, action string, details map[string]string) error {
	return b.Audit.Record(ctx, application.AuditEntry{UserID: userID, ActorID: userID, Action: action, Details: details})
}
