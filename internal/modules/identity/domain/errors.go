package domain

import "github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"

var (
	ErrInvalidName  = apperrors.Validation("INVALID_NAME", "name is required")
	ErrNameTooLong  = apperrors.Validationf("NAME_TOO_LONG", "name must be at most %d characters", MaxNameLength)
	ErrInvalidEmail = apperrors.Validation("INVALID_EMAIL", "invalid email")

	ErrEmailTaken       = apperrors.Conflict("EMAIL_TAKEN", "an account with this email already exists")
	ErrEmailNotVerified = apperrors.Conflict("EMAIL_NOT_VERIFIED", "an account with this email already exists; verify the email address in Firebase to sign in to it")
	ErrFirebaseUIDTaken = apperrors.Conflict("FIREBASE_UID_TAKEN", "this Firebase user is already linked to an account")
	ErrAccountNotLinked = apperrors.Conflict("ACCOUNT_NOT_LINKED", "sign in with Google or email and password once before using a passkey")
	ErrPasskeyLoginOff  = apperrors.Forbidden("PASSKEY_LOGIN_DISABLED", "passkey sign-in is not enabled on this server")
	ErrNoEmailOnIDToken = apperrors.Validation("EMAIL_REQUIRED", "the Firebase account has no email address")

	ErrInvalidIDToken       = apperrors.Unauthorized("INVALID_ACCESS_TOKEN", "invalid Firebase ID token")
	ErrExpiredIDToken       = apperrors.Unauthorized("EXPIRED_ACCESS_TOKEN", "Firebase ID token has expired")
	ErrTokenRevoked         = apperrors.Unauthorized("TOKEN_REVOKED", "this sign-in has been revoked; sign in again")
	ErrAccountNotRegistered = apperrors.Unauthorized("ACCOUNT_NOT_REGISTERED", "no account for this Firebase user; call POST /auth first")

	ErrPasskeyNameTooLong       = apperrors.Validationf("PASSKEY_NAME_TOO_LONG", "passkey name must be at most %d characters", MaxPasskeyNameLength)
	ErrInvalidPasskeyChallenge  = apperrors.Validation("INVALID_PASSKEY_CHALLENGE", "invalid or expired passkey challenge")
	ErrPasskeyRejected          = apperrors.Validation("PASSKEY_REJECTED", "the passkey could not be verified")
	ErrPasskeyExists            = apperrors.Conflict("PASSKEY_EXISTS", "this passkey is already registered")
	ErrTooManyPasskeys          = apperrors.Conflictf("TOO_MANY_PASSKEYS", "an account can have at most %d passkeys", MaxPasskeys)
	ErrPasskeyNotFound          = apperrors.NotFound("PASSKEY_NOT_FOUND", "passkey not found")
	ErrInvalidPasskeyCredential = apperrors.Unauthorized("INVALID_PASSKEY", "passkey sign-in failed")
)
