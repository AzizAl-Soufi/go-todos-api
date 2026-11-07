-- +goose Up
-- Collections group projects; projects (lists) group todos. Todos gain a
-- status (replacing the completed flag), priority, deadline, notes and tags.

CREATE TABLE IF NOT EXISTS collections (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    icon        TEXT        NOT NULL DEFAULT '',
    color       TEXT        NOT NULL DEFAULT 'default',
    archived_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_collections_user_created ON collections (user_id, created_at);

CREATE TABLE IF NOT EXISTS projects (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    collection_id UUID        REFERENCES collections(id) ON DELETE SET NULL,
    name          TEXT        NOT NULL,
    description   TEXT        NOT NULL DEFAULT '',
    icon          TEXT        NOT NULL DEFAULT '',
    color         TEXT        NOT NULL DEFAULT 'default',
    archived_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_projects_user_created ON projects (user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_projects_collection   ON projects (collection_id);

ALTER TABLE todos
    ADD COLUMN project_id   UUID        REFERENCES projects(id) ON DELETE CASCADE,
    ADD COLUMN notes        TEXT        NOT NULL DEFAULT '',
    ADD COLUMN status       TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'in_progress', 'blocked', 'done', 'cancelled')),
    ADD COLUMN priority     SMALLINT    NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 4),
    ADD COLUMN tags         TEXT[]      NOT NULL DEFAULT '{}',
    ADD COLUMN due_at       TIMESTAMPTZ,
    ADD COLUMN completed_at TIMESTAMPTZ,
    ADD COLUMN updated_at   TIMESTAMPTZ;

UPDATE todos SET
    status       = CASE WHEN completed THEN 'done' ELSE 'pending' END,
    completed_at = CASE WHEN completed THEN created_at END,
    updated_at   = created_at;

ALTER TABLE todos
    ALTER COLUMN updated_at SET NOT NULL,
    ALTER COLUMN updated_at SET DEFAULT NOW(),
    DROP COLUMN completed;

CREATE INDEX IF NOT EXISTS idx_todos_project  ON todos (project_id);
CREATE INDEX IF NOT EXISTS idx_todos_user_due ON todos (user_id, due_at);
CREATE INDEX IF NOT EXISTS idx_todos_tags     ON todos USING GIN (tags);

-- +goose Down
DROP INDEX IF EXISTS idx_todos_tags;
DROP INDEX IF EXISTS idx_todos_user_due;
DROP INDEX IF EXISTS idx_todos_project;

ALTER TABLE todos ADD COLUMN completed BOOLEAN NOT NULL DEFAULT false;
UPDATE todos SET completed = (status = 'done');
ALTER TABLE todos
    DROP COLUMN updated_at,
    DROP COLUMN completed_at,
    DROP COLUMN due_at,
    DROP COLUMN tags,
    DROP COLUMN priority,
    DROP COLUMN status,
    DROP COLUMN notes,
    DROP COLUMN project_id;

DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS collections;
