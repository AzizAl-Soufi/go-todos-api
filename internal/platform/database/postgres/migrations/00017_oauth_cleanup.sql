-- +goose Up
-- The oauth cleanup job deletes grants whose refresh token expired and
-- registered clients that are old and unused: no grant references them
-- (oauth_grants.client_id) and they were created before a cutoff
-- (MongoDB has the same indexes, migration 14).
CREATE INDEX IF NOT EXISTS idx_oauth_grants_refresh_expires_at ON oauth_grants (refresh_expires_at);
CREATE INDEX IF NOT EXISTS idx_oauth_grants_client_id          ON oauth_grants (client_id);
CREATE INDEX IF NOT EXISTS idx_oauth_clients_created_at        ON oauth_clients (created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_oauth_clients_created_at;
DROP INDEX IF EXISTS idx_oauth_grants_client_id;
DROP INDEX IF EXISTS idx_oauth_grants_refresh_expires_at;
