-- +goose Up
-- Passkeys (WebAuthn credentials) and pending passkey ceremonies (identity module).

-- credential is the relying party's credential record (public key, sign
-- counter, flags) as stored by the WebAuthn adapter; it changes on every use.
CREATE TABLE IF NOT EXISTS passkeys (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT        NOT NULL,
    credential_id BYTEA       NOT NULL UNIQUE,
    credential    BYTEA       NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_passkeys_user_created ON passkeys (user_id, created_at, id);

-- One row per handed-out challenge, redeemed once; only the token's SHA-256
-- is stored. user_id is set for registrations and NULL for sign-ins. Expired
-- rows are pruned whenever a new challenge is stored.
CREATE TABLE IF NOT EXISTS passkey_challenges (
    token_hash TEXT        PRIMARY KEY,
    user_id    UUID        REFERENCES users(id) ON DELETE CASCADE,
    state      BYTEA       NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_passkey_challenges_expires_at ON passkey_challenges (expires_at);
CREATE INDEX IF NOT EXISTS idx_passkey_challenges_user_id    ON passkey_challenges (user_id);

-- +goose Down
DROP TABLE IF EXISTS passkey_challenges;
DROP TABLE IF EXISTS passkeys;
