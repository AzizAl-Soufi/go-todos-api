-- +goose Up
-- Devices that receive push notifications (notifications module): one row
-- per FCM registration token. A token belongs to one user; registering it
-- again moves it. user_id has no foreign key, like other IDs that cross
-- modules; the account's deletion removes its devices.
CREATE TABLE IF NOT EXISTS devices (
    token      TEXT        PRIMARY KEY,
    user_id    UUID        NOT NULL,
    platform   TEXT        NOT NULL CHECK (platform IN ('android', 'ios', 'web')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_devices_user_updated ON devices (user_id, updated_at);

-- +goose Down
DROP TABLE IF EXISTS devices;
