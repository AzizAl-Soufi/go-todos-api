-- Queries owned by the notifications module. Regenerate with `make sqlc`.

-- A token registered again moves to the new user and keeps created_at.
-- name: SaveDevice :exec
INSERT INTO devices (token, user_id, platform, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (token) DO UPDATE
SET user_id    = EXCLUDED.user_id,
    platform   = EXCLUDED.platform,
    updated_at = EXCLUDED.updated_at;

-- name: ListUserDevices :many
SELECT * FROM devices WHERE user_id = $1 ORDER BY updated_at, token;

-- name: ListDevicesOfUsers :many
SELECT * FROM devices WHERE user_id = ANY(@user_ids::uuid[]) ORDER BY updated_at, token;

-- name: DeleteDevice :execrows
DELETE FROM devices WHERE token = $1 AND user_id = $2;

-- name: DeleteDeviceTokens :exec
DELETE FROM devices WHERE token = ANY(@tokens::text[]);

-- name: DeleteUserDevices :exec
DELETE FROM devices WHERE user_id = $1;
