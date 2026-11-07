package domain

import "github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"

var (
	ErrInvalidName     = apperrors.Validation("INVALID_NAME", "name is required")
	ErrNameTooLong     = apperrors.Validationf("NAME_TOO_LONG", "name must be at most %d characters", MaxNameLength)
	ErrInvalidEmail    = apperrors.Validation("INVALID_EMAIL", "invalid email")
	ErrWeakPassword    = apperrors.Validationf("WEAK_PASSWORD", "password must be at least %d characters", MinPasswordLength)
	ErrPasswordTooLong = apperrors.Validationf("PASSWORD_TOO_LONG", "password must be at most %d bytes", MaxPasswordBytes)

	ErrEmailTaken         = apperrors.Conflict("EMAIL_TAKEN", "an account with this email already exists")
	ErrInvalidCredentials = apperrors.Unauthorized("INVALID_CREDENTIALS", "invalid email or password")

	ErrInvalidAccessToken   = apperrors.Unauthorized("INVALID_ACCESS_TOKEN", "invalid access token")
	ErrMalformedAccessToken = apperrors.Unauthorized("MALFORMED_ACCESS_TOKEN", "malformed access token")
	ErrExpiredAccessToken   = apperrors.Unauthorized("EXPIRED_ACCESS_TOKEN", "access token has expired")
	ErrTokenRevoked         = apperrors.Unauthorized("TOKEN_REVOKED", "access token has been revoked")

	ErrInvalidRefreshToken = apperrors.Unauthorized("INVALID_REFRESH_TOKEN", "invalid or expired refresh token")
	ErrRefreshTokenReused  = apperrors.Unauthorized("REFRESH_TOKEN_REUSED", "refresh token was already used; this login has been signed out everywhere")

	ErrInvalidVerificationToken = apperrors.Validation("INVALID_VERIFICATION_TOKEN", "invalid or expired verification token")
	ErrAlreadyVerified          = apperrors.Conflict("EMAIL_ALREADY_VERIFIED", "email address is already verified")

	ErrInvalidResetToken = apperrors.Validation("INVALID_RESET_TOKEN", "invalid or expired password reset token")
	ErrIncorrectPassword = apperrors.Validation("INCORRECT_PASSWORD", "current password is incorrect")
)
