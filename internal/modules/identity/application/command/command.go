// Package command holds the identity module's write use cases. Each command
// is a plain struct, handled by its own handler. Handlers return nothing but
// what only the write can produce: the new user's ID and freshly issued
// credentials. Callers read everything else back through the query side.
package command

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/identity/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/tx"
	"github.com/google/uuid"
)

type Config struct {
	RefreshTokenTTL  time.Duration
	VerificationTTL  time.Duration
	PasswordResetTTL time.Duration
	// VerificationURL and PasswordResetURL are link bases; the token is
	// appended as ?token=... The reset link should open a client page that
	// asks for the new password and calls POST /auth/password/reset.
	VerificationURL  string
	PasswordResetURL string
}

func DefaultConfig(baseURL string) Config {
	return Config{
		RefreshTokenTTL:  7 * 24 * time.Hour,
		VerificationTTL:  24 * time.Hour,
		PasswordResetTTL: time.Hour,
		VerificationURL:  baseURL + "/auth/verify-email",
		PasswordResetURL: baseURL + "/reset-password",
	}
}

type Deps struct {
	Users          application.UserRepository
	Sessions       application.SessionRepository
	Verifications  application.OneTimeTokenRepository
	PasswordResets application.OneTimeTokenRepository
	Hasher         application.PasswordHasher
	Tokens         application.AccessTokenIssuer
	Mailer         application.Mailer
	Todos          application.TodoDeleter
	Tx             tx.Runner
	Config         Config
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Handlers are all identity commands.
type Handlers struct {
	Register             RegisterHandler
	Login                LoginHandler
	Refresh              RefreshHandler
	Logout               LogoutHandler
	LogoutAll            LogoutAllHandler
	VerifyEmail          VerifyEmailHandler
	ResendVerification   ResendVerificationHandler
	RequestPasswordReset RequestPasswordResetHandler
	ResetPassword        ResetPasswordHandler
	ChangePassword       ChangePasswordHandler
	DeleteAccount        DeleteAccountHandler
}

func New(d Deps) Handlers {
	if d.Tx == nil {
		d.Tx = tx.None{}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	b := &base{Deps: d}
	return Handlers{
		Register: RegisterHandler{b}, Login: LoginHandler{b}, Refresh: RefreshHandler{b},
		Logout: LogoutHandler{b}, LogoutAll: LogoutAllHandler{b},
		VerifyEmail: VerifyEmailHandler{b}, ResendVerification: ResendVerificationHandler{b},
		RequestPasswordReset: RequestPasswordResetHandler{b}, ResetPassword: ResetPasswordHandler{b},
		ChangePassword: ChangePasswordHandler{b}, DeleteAccount: DeleteAccountHandler{b},
	}
}

// Tokens is the credential pair issued on registration, login, refresh and
// password change.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
}

// Credentials is what registration and login produce.
type Credentials struct {
	UserID string
	Tokens Tokens
}

// base is what every handler shares: the dependencies and the steps several
// commands reuse.
type base struct {
	Deps

	dummyHashOnce sync.Once
	dummyHash     string
}

// startSession creates a refresh-token session (in familyID, or a new family)
// and issues an access token.
func (b *base) startSession(ctx context.Context, user *domain.User, familyID string) (Tokens, error) {
	now := b.Now()
	if familyID == "" {
		familyID = uuid.NewString()
	}
	refresh, refreshHash, err := newOpaqueToken()
	if err != nil {
		return Tokens{}, err
	}
	sess := &domain.Session{
		UserID: user.ID, FamilyID: familyID, TokenHash: refreshHash,
		ExpiresAt: now.Add(b.Config.RefreshTokenTTL), CreatedAt: now,
	}
	if err := b.Sessions.Create(ctx, sess); err != nil {
		return Tokens{}, err
	}
	access, expiresAt, err := b.Tokens.Issue(user, now)
	if err != nil {
		return Tokens{}, apperrors.InternalWrap("TOKEN_GENERATION_FAILED", "failed to generate tokens", err)
	}
	return Tokens{AccessToken: access, RefreshToken: refresh, ExpiresIn: expiresAt.Sub(now)}, nil
}

// issueToken stores a fresh one-time token for userID in repo, replacing any
// pending one, and returns the link (linkBase?token=...) to email.
func (b *base) issueToken(ctx context.Context, repo application.OneTimeTokenRepository, userID string, ttl time.Duration, linkBase string) (string, error) {
	now := b.Now()
	token, hash, err := newOpaqueToken()
	if err != nil {
		return "", err
	}
	t := &domain.OneTimeToken{UserID: userID, TokenHash: hash, ExpiresAt: now.Add(ttl), CreatedAt: now}
	if err := repo.Save(ctx, t); err != nil {
		return "", err
	}
	return linkBase + "?token=" + url.QueryEscape(token), nil
}

// consumeToken redeems a one-time token, reporting invalid when it is
// unknown, used or expired.
func (b *base) consumeToken(ctx context.Context, repo application.OneTimeTokenRepository, token string, invalid error) (*domain.OneTimeToken, error) {
	if token == "" {
		return nil, invalid
	}
	t, err := repo.Consume(ctx, hashToken(token))
	if err != nil {
		return nil, apperrors.NotFoundAs(err, invalid)
	}
	if t.Expired(b.Now()) {
		return nil, invalid
	}
	return t, nil
}

func (b *base) hashPassword(password string) (string, error) {
	if err := domain.ValidatePassword(password); err != nil {
		return "", err
	}
	hash, err := b.Hasher.Hash(password)
	if err != nil {
		return "", apperrors.InternalWrap("PASSWORD_HASH_FAILED", "failed to hash password", err)
	}
	return hash, nil
}

// checkPassword compares a password, failing the same way and taking about
// the same time for unknown users and accounts without a password.
func (b *base) checkPassword(user *domain.User, password string, fail error) error {
	if user == nil || !user.HasPassword() {
		b.dummyHashOnce.Do(func() { b.dummyHash, _ = b.Hasher.Hash("timing-equalizer-password") })
		_ = b.Hasher.Compare(b.dummyHash, password)
		return fail
	}
	if b.Hasher.Compare(user.PasswordHash, password) != nil {
		return fail
	}
	return nil
}

// findByEmail returns nil (and no error) when no account has this email.
func (b *base) findByEmail(ctx context.Context, email string) (*domain.User, error) {
	email, err := domain.NormalizeEmail(email)
	if err != nil {
		return nil, nil
	}
	user, err := b.Users.FindByEmail(ctx, email)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, nil
	}
	return user, err
}

// revokeEverything invalidates every credential of the user: access tokens
// (token version), refresh tokens and any pending password reset link.
func (b *base) revokeEverything(ctx context.Context, userID string) error {
	if err := b.Users.IncrementTokenVersion(ctx, userID); err != nil {
		return err
	}
	if err := b.Sessions.RevokeAllForUser(ctx, userID, b.Now()); err != nil {
		return err
	}
	return b.PasswordResets.DeleteForUser(ctx, userID)
}

// sendVerification never fails the command: the user can ask for a new link.
func (b *base) sendVerification(ctx context.Context, user *domain.User, link string) {
	if err := b.Mailer.SendVerificationEmail(ctx, user.Email, user.Name, link); err != nil {
		slog.Error("failed to send verification email", "user_id", user.ID, "error", err)
	}
}

// newOpaqueToken returns a random URL-safe token and its SHA-256 hex digest.
// Only the digest is stored, so a database leak does not expose usable tokens.
func newOpaqueToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", apperrors.InternalWrap("RANDOM_FAILED", "failed to generate token", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
