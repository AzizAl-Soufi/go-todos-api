-- +goose Up
-- Password authentication, token versioning and email verification.
-- Existing users get an empty password hash and therefore cannot log in until
-- they set a password; token_version starts at 0.
ALTER TABLE users
    ADD COLUMN password_hash     TEXT        NOT NULL DEFAULT '',
    ADD COLUMN email_verified_at TIMESTAMPTZ,
    ADD COLUMN token_version     INTEGER     NOT NULL DEFAULT 0;

-- Refresh-token sessions. Only a SHA-256 hash of each token is stored.
-- Rotation keeps one family per login; reusing a revoked token revokes the family.
CREATE TABLE IF NOT EXISTS sessions (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id   UUID        NOT NULL,
    token_hash  TEXT        NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id   ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_family_id ON sessions (family_id);

-- At most one pending verification per user; the token is stored hashed.
CREATE TABLE IF NOT EXISTS email_verifications (
    user_id    UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS email_verifications;
DROP TABLE IF EXISTS sessions;
ALTER TABLE users
    DROP COLUMN IF EXISTS token_version,
    DROP COLUMN IF EXISTS email_verified_at,
    DROP COLUMN IF EXISTS password_hash;
