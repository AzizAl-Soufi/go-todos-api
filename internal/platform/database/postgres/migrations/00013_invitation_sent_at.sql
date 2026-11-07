-- +goose Up
-- When each team invitation email was last sent (teams module): resending
-- waits 2 minutes after the last send. The default keeps inserts from the
-- previous release working during a rolling deploy; existing invitations
-- were last sent when they were created.
ALTER TABLE team_invitations ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
UPDATE team_invitations SET sent_at = created_at;

-- +goose Down
ALTER TABLE team_invitations DROP COLUMN IF EXISTS sent_at;
