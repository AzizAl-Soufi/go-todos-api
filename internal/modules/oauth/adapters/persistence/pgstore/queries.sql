-- Queries owned by the oauth module. Regenerate with `make sqlc`.

-- clients

-- name: CreateClient :one
INSERT INTO oauth_clients (name, redirect_uris, created_at) VALUES ($1, $2, $3) RETURNING id;

-- name: GetClient :one
SELECT * FROM oauth_clients WHERE id = $1;

-- codes

-- name: DeleteExpiredCodes :exec
DELETE FROM oauth_codes WHERE expires_at < $1;

-- name: CreateCode :exec
INSERT INTO oauth_codes (code_hash, client_id, client_name, redirect_uri, challenge, resource, scope,
                         user_id, user_name, user_email, email_verified, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- Single-use: the row is deleted as it is read.
-- name: ConsumeCode :one
DELETE FROM oauth_codes WHERE code_hash = $1 RETURNING *;

-- name: DeleteUserCodes :exec
DELETE FROM oauth_codes WHERE user_id = $1;

-- grants

-- Grants whose refresh token expired are of no use; creating a grant clears
-- the user's.
-- name: DeleteExpiredUserGrants :exec
DELETE FROM oauth_grants WHERE user_id = $1 AND refresh_expires_at < $2;

-- name: CreateGrant :one
INSERT INTO oauth_grants (client_id, client_name, scope, resource, user_id, user_name, user_email, email_verified,
                          access_hash, access_expires_at, refresh_hash, refresh_expires_at, created_at, last_used_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING id;

-- name: GetGrant :one
SELECT * FROM oauth_grants WHERE id = $1;

-- name: ListUserGrants :many
SELECT * FROM oauth_grants WHERE user_id = $1 ORDER BY last_used_at DESC, id DESC;

-- Rotation only matches while the presented refresh token is current.
-- name: RotateGrant :execrows
UPDATE oauth_grants
SET access_hash = @access_hash, access_expires_at = @access_expires_at,
    refresh_hash = @new_refresh_hash, refresh_expires_at = @refresh_expires_at, last_used_at = @used_at
WHERE id = @id AND refresh_hash = @refresh_hash;

-- name: DeleteGrant :execrows
DELETE FROM oauth_grants WHERE id = $1 AND user_id = $2;

-- name: RevokeGrant :exec
DELETE FROM oauth_grants WHERE id = $1;

-- name: DeleteUserGrants :exec
DELETE FROM oauth_grants WHERE user_id = $1;
