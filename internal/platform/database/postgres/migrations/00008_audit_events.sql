-- +goose Up
-- Append-only audit log (audit module). An event belongs either to a team
-- (team_id) or to one user's security log (user_id). Pages are read newest
-- first by (created_at, id).
CREATE TABLE IF NOT EXISTS audit_events (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id     UUID,
    user_id     UUID,
    actor_id    UUID,
    action      TEXT        NOT NULL,
    target_type TEXT        NOT NULL DEFAULT '',
    target_id   TEXT        NOT NULL DEFAULT '',
    details     JSONB       NOT NULL DEFAULT '{}',
    ip          TEXT        NOT NULL DEFAULT '',
    user_agent  TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((team_id IS NULL) <> (user_id IS NULL))
);
CREATE INDEX IF NOT EXISTS idx_audit_events_team ON audit_events (team_id, created_at DESC, id DESC) WHERE team_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_audit_events_user ON audit_events (user_id, created_at DESC, id DESC) WHERE user_id IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS audit_events;
