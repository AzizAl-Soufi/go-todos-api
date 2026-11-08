-- Queries owned by the teams module. Regenerate with `make sqlc`.

-- teams

-- name: CreateTeam :one
INSERT INTO teams (name, created_at, updated_at) VALUES (@name, @created_at, @updated_at)
RETURNING *;

-- name: GetTeam :one
SELECT * FROM teams WHERE id = @id;

-- name: UpdateTeam :execrows
UPDATE teams SET name = @name, updated_at = @updated_at WHERE id = @id;

-- name: DeleteTeam :execrows
DELETE FROM teams WHERE id = @id;

-- members

-- name: AddMember :exec
INSERT INTO team_members (team_id, user_id, name, email, role, joined_at)
VALUES (@team_id, @user_id, @name, @email, @role, @joined_at);

-- name: GetMember :one
SELECT * FROM team_members WHERE team_id = @team_id AND user_id = @user_id;

-- name: SetMemberRole :execrows
UPDATE team_members SET role = @role WHERE team_id = @team_id AND user_id = @user_id;

-- name: RemoveMember :execrows
DELETE FROM team_members WHERE team_id = @team_id AND user_id = @user_id;

-- name: ListMembersByTeam :many
SELECT * FROM team_members WHERE team_id = @team_id ORDER BY joined_at, team_id, user_id;

-- name: ListMembersByUser :many
SELECT * FROM team_members WHERE user_id = @user_id ORDER BY joined_at, team_id, user_id;

-- name: DeleteMembersByTeam :exec
DELETE FROM team_members WHERE team_id = @team_id;

-- invitations

-- Saving replaces the pending invitation for the same team and email.
-- name: SaveInvitation :one
INSERT INTO team_invitations (team_id, email, role, invited_by, token_hash, expires_at, created_at, sent_at)
VALUES (@team_id, @email, @role, @invited_by, @token_hash, @expires_at, @created_at, @sent_at)
ON CONFLICT (team_id, email) DO UPDATE
SET id = gen_random_uuid(), role = EXCLUDED.role, invited_by = EXCLUDED.invited_by, token_hash = EXCLUDED.token_hash,
    expires_at = EXCLUDED.expires_at, created_at = EXCLUDED.created_at, sent_at = EXCLUDED.sent_at
RETURNING id;

-- Resending gives the invitation a new link, expiry and send time, unless
-- it was sent after @sent_before (the cooldown) or is gone: then no row.
-- name: ResendInvitation :execrows
UPDATE team_invitations
SET token_hash = @token_hash, expires_at = @expires_at, sent_at = @sent_at
WHERE team_id = @team_id AND id = @id AND sent_at <= @sent_before;

-- name: GetInvitation :one
SELECT * FROM team_invitations WHERE team_id = @team_id AND id = @id;

-- name: FindInvitationByTokenHash :one
SELECT * FROM team_invitations WHERE token_hash = @token_hash;

-- name: DeleteInvitation :execrows
DELETE FROM team_invitations WHERE team_id = @team_id AND id = @id;

-- name: ListInvitationsByTeam :many
SELECT * FROM team_invitations WHERE team_id = @team_id ORDER BY created_at, id;

-- name: DeleteInvitationsByTeam :exec
DELETE FROM team_invitations WHERE team_id = @team_id;

-- Bounded, so the periodic cleanup never holds locks for long.
-- name: DeleteExpiredInvitations :execrows
DELETE FROM team_invitations WHERE id IN (
    SELECT i.id FROM team_invitations i WHERE i.expires_at < @before ORDER BY i.expires_at LIMIT @lim
);
