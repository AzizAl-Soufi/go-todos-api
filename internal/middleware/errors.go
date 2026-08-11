package middleware

import apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/shared/errors"

var (
	ErrExpiredAccessToken = apperrors.Unauthorized(
		"EXPIRED_ACCESS_TOKEN",
		"invalid or expired access token",
	)

	ErrUnauthorizedContext = apperrors.Unauthorized(
		"UNAUTHORIZED",
		"Unauthorized: User data not found in context",
	)

	ErrExpiredRefreshToken = apperrors.Unauthorized(
		"EXPIRED_REFRESH_TOKEN",
		"invalid or expired refresh token",
	)

	ErrInvalidAccessToken = apperrors.Unauthorized(
		"INVALID_ACCESS_TOKEN",
		"invalid access token",
	)

	ErrUnauthorized = apperrors.Unauthorized(
		"UNAUTHORIZED",
		"unauthorized request",
	)

	ErrInvalidRefreshTokenType = apperrors.Unauthorized(
		"INVALID_TOKEN_TYPE",
		"invalid token type: expected refresh token",
	)

	ErrMissingAccessToken = apperrors.Unauthorized(
		"MISSING_ACCESS_TOKEN",
		"access token is missing",
	)

	ErrMissingAuthHeader = apperrors.Unauthorized(
		"MISSING_AUTH_HEADER",
		"authorization header is missing",
	)
)
