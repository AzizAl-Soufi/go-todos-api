-- Queries owned by the audit module. Regenerate with `make sqlc`.

-- name: AppendEvent :one
INSERT INTO audit_events (team_id, user_id, actor_id, action, target_type, target_id, details, ip, user_agent, created_at)
VALUES (sqlc.narg(team_id), sqlc.narg(user_id), sqlc.narg(actor_id), @action, @target_type, @target_id, @details, @ip, @user_agent, @created_at)
RETURNING id;

-- The filter mirrors domain.Filter: one log, after the cursor, newest first.
-- name: ListEvents :many
SELECT * FROM audit_events
WHERE (sqlc.narg(team_id)::uuid IS NULL OR team_id = sqlc.narg(team_id)::uuid)
  AND (sqlc.narg(user_id)::uuid IS NULL OR user_id = sqlc.narg(user_id)::uuid)
  AND (sqlc.narg(before_at)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(before_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT @max_rows;

-- name: DeleteEventsByUser :exec
DELETE FROM audit_events WHERE user_id = @user_id;

-- name: DeleteEventsByTeam :exec
DELETE FROM audit_events WHERE team_id = @team_id;
