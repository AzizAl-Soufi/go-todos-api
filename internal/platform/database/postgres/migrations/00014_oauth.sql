-- +goose Up
-- The OAuth 2.1 authorization server for MCP clients (oauth module).
-- Clients registered through Dynamic Client Registration; clients that use a
-- Client ID Metadata Document aren't stored. Codes and tokens are stored as
-- SHA-256 hashes only. user_id has no foreign key, like other IDs that cross
-- modules: logging out everywhere and deleting the account remove the user's
-- codes and grants.
CREATE TABLE IF NOT EXISTS oauth_clients (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT        NOT NULL,
    redirect_uris TEXT[]      NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS oauth_codes (
    code_hash      TEXT        PRIMARY KEY,
    client_id      TEXT        NOT NULL,
    client_name    TEXT        NOT NULL,
    redirect_uri   TEXT        NOT NULL,
    challenge      TEXT        NOT NULL,
    resource       TEXT        NOT NULL,
    scope          TEXT        NOT NULL,
    user_id        UUID        NOT NULL,
    user_name      TEXT        NOT NULL,
    user_email     TEXT        NOT NULL,
    email_verified BOOLEAN     NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_oauth_codes_expires_at ON oauth_codes (expires_at);
CREATE INDEX IF NOT EXISTS idx_oauth_codes_user_id    ON oauth_codes (user_id);

-- A grant is a connected app: one per approval, with its current tokens.
CREATE TABLE IF NOT EXISTS oauth_grants (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id          TEXT        NOT NULL,
    client_name        TEXT        NOT NULL,
    scope              TEXT        NOT NULL,
    resource           TEXT        NOT NULL,
    user_id            UUID        NOT NULL,
    user_name          TEXT        NOT NULL,
    user_email         TEXT        NOT NULL,
    email_verified     BOOLEAN     NOT NULL,
    access_hash        TEXT        NOT NULL,
    access_expires_at  TIMESTAMPTZ NOT NULL,
    refresh_hash       TEXT        NOT NULL,
    refresh_expires_at TIMESTAMPTZ NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_oauth_grants_user_used ON oauth_grants (user_id, last_used_at DESC);

-- +goose Down
DROP TABLE IF EXISTS oauth_grants;
DROP TABLE IF EXISTS oauth_codes;
DROP TABLE IF EXISTS oauth_clients;
