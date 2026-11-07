-- +goose Up
-- Pending password resets: at most one per user, single-use, stored hashed.
CREATE TABLE IF NOT EXISTS password_resets (
    user_id    UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS password_resets;
