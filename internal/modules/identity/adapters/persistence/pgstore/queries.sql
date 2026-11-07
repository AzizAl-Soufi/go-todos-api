-- Queries owned by the identity module. Regenerate with `make sqlc`.

-- users

-- name: CreateUser :one
INSERT INTO users (name, email, firebase_uid, email_verified_at, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByFirebaseUID :one
SELECT * FROM users WHERE firebase_uid = $1;

-- Only links an account that has no Firebase user yet.
-- name: LinkFirebaseUID :execrows
UPDATE users SET firebase_uid = $2 WHERE id = $1 AND firebase_uid IS NULL;

-- Keeps the first verification time if the email was already verified.
-- name: MarkEmailVerified :execrows
UPDATE users
SET email_verified_at = COALESCE(email_verified_at, $2)
WHERE id = $1;

-- name: RevokeUserSessions :execrows
UPDATE users SET sessions_valid_after = $2 WHERE id = $1;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;

-- passkeys

-- name: CreatePasskey :one
INSERT INTO passkeys (user_id, name, credential_id, credential, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: ListUserPasskeys :many
SELECT * FROM passkeys WHERE user_id = $1 ORDER BY created_at, id;

-- name: RecordPasskeyUse :execrows
UPDATE passkeys SET credential = $2, last_used_at = $3 WHERE id = $1;

-- name: DeletePasskey :execrows
DELETE FROM passkeys WHERE id = $1 AND user_id = $2;

-- name: DeleteUserPasskeys :exec
DELETE FROM passkeys WHERE user_id = $1;

-- passkey challenges

-- name: DeleteExpiredPasskeyChallenges :exec
DELETE FROM passkey_challenges WHERE expires_at < $1;

-- name: CreatePasskeyChallenge :exec
INSERT INTO passkey_challenges (token_hash, user_id, state, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5);

-- Single-use: the row is deleted as it is read.
-- name: ConsumePasskeyChallenge :one
DELETE FROM passkey_challenges WHERE token_hash = $1 RETURNING *;

-- name: DeleteUserPasskeyChallenges :exec
DELETE FROM passkey_challenges WHERE user_id = $1;
