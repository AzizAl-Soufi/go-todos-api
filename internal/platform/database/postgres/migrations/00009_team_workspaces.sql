-- +goose Up
-- Collections, projects and todos can belong to a team (team_id); user_id
-- stays their creator. Team records outlive their creator's account, so the
-- foreign keys to users are dropped: account deletion removes a user's
-- personal records explicitly, in one transaction, as on MongoDB.
ALTER TABLE collections ADD COLUMN team_id UUID;
ALTER TABLE projects    ADD COLUMN team_id UUID;
ALTER TABLE todos       ADD COLUMN team_id UUID;

ALTER TABLE todos       DROP CONSTRAINT IF EXISTS fk_user;
ALTER TABLE projects    DROP CONSTRAINT IF EXISTS projects_user_id_fkey;
ALTER TABLE collections DROP CONSTRAINT IF EXISTS collections_user_id_fkey;

CREATE INDEX IF NOT EXISTS idx_collections_team_created ON collections (team_id, created_at) WHERE team_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_projects_team_created    ON projects (team_id, created_at) WHERE team_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_todos_team               ON todos (team_id) WHERE team_id IS NOT NULL;

-- +goose Down
-- Team records can't go back to having no team, and personal records of
-- deleted accounts can't satisfy the restored foreign keys: both are removed.
DELETE FROM todos       WHERE team_id IS NOT NULL OR user_id NOT IN (SELECT id FROM users);
DELETE FROM projects    WHERE team_id IS NOT NULL OR user_id NOT IN (SELECT id FROM users);
DELETE FROM collections WHERE team_id IS NOT NULL OR user_id NOT IN (SELECT id FROM users);

DROP INDEX IF EXISTS idx_todos_team;
DROP INDEX IF EXISTS idx_projects_team_created;
DROP INDEX IF EXISTS idx_collections_team_created;

ALTER TABLE collections ADD CONSTRAINT collections_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE projects    ADD CONSTRAINT projects_user_id_fkey    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE todos       ADD CONSTRAINT fk_user                  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE todos       DROP COLUMN team_id;
ALTER TABLE projects    DROP COLUMN team_id;
ALTER TABLE collections DROP COLUMN team_id;
