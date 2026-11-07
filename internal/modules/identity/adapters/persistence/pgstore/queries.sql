-- Queries owned by the identity module. Regenerate with `make sqlc`.

-- users

-- name: CreateUser :one
INSERT INTO users (name, email, password_hash, created_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- Keeps the first verification time if the email was already verified.
-- name: MarkEmailVerified :execrows
UPDATE users
SET email_verified_at = COALESCE(email_verified_at, $2)
WHERE id = $1;

-- name: IncrementTokenVersion :execrows
UPDATE users SET token_version = token_version + 1 WHERE id = $1;

-- name: SetPasswordHash :execrows
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;

-- sessions

-- name: CreateSession :one
INSERT INTO sessions (user_id, family_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = $1;

-- Conditional on revoked_at IS NULL, so exactly one concurrent caller wins.
-- name: RevokeSession :execrows
UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSessionFamily :exec
UPDATE sessions SET revoked_at = $2 WHERE family_id = $1 AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL;

-- name: DeleteUserSessions :exec
DELETE FROM sessions WHERE user_id = $1;

-- email verifications

-- name: UpsertEmailVerification :exec
INSERT INTO email_verifications (user_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET token_hash = EXCLUDED.token_hash,
    expires_at = EXCLUDED.expires_at,
    created_at = EXCLUDED.created_at;

-- Single-use: the row is deleted as it is read.
-- name: ConsumeEmailVerification :one
DELETE FROM email_verifications WHERE token_hash = $1 RETURNING *;

-- name: DeleteEmailVerification :exec
DELETE FROM email_verifications WHERE user_id = $1;

-- password resets (same shape and rules as email verifications)

-- name: UpsertPasswordReset :exec
INSERT INTO password_resets (user_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET token_hash = EXCLUDED.token_hash,
    expires_at = EXCLUDED.expires_at,
    created_at = EXCLUDED.created_at;

-- Single-use: the row is deleted as it is read.
-- name: ConsumePasswordReset :one
DELETE FROM password_resets WHERE token_hash = $1 RETURNING *;

-- name: DeletePasswordReset :exec
DELETE FROM password_resets WHERE user_id = $1;
