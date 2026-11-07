-- +goose Up
-- Every todos query filters by owner and lists newest first (MongoDB has the
-- same index, idx_user_created). It also speeds up the users(id) cascade.
CREATE INDEX IF NOT EXISTS idx_todos_user_created ON todos (user_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_todos_user_created;
