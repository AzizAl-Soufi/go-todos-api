-- Queries owned by the todos module. Regenerate with `make sqlc`.

-- todos

-- name: CreateTodo :one
INSERT INTO todos (user_id, project_id, title, notes, status, priority, tags, due_at, completed_at, created_at, updated_at)
VALUES (@user_id, sqlc.narg(project_id), @title, @notes, @status, @priority, @tags, sqlc.narg(due_at), sqlc.narg(completed_at), @created_at, @updated_at)
RETURNING *;

-- name: GetTodo :one
SELECT * FROM todos WHERE id = @id AND user_id = @user_id;

-- name: UpdateTodo :execrows
UPDATE todos
SET project_id = sqlc.narg(project_id), title = @title, notes = @notes, status = @status,
    priority = @priority, tags = @tags, due_at = sqlc.narg(due_at),
    completed_at = sqlc.narg(completed_at), updated_at = @updated_at
WHERE id = @id AND user_id = @user_id;

-- name: DeleteTodo :execrows
DELETE FROM todos WHERE id = @id AND user_id = @user_id;

-- Every filter is off when its parameter is empty or NULL; the semantics
-- mirror domain.Query (the store contract tests check they agree).
-- name: ListTodos :many
SELECT * FROM todos
WHERE user_id = @user_id
  AND (sqlc.narg(project_id)::uuid IS NULL OR project_id = sqlc.narg(project_id)::uuid)
  AND (NOT @inbox::boolean OR project_id IS NULL)
  AND (project_id IS NULL OR NOT (project_id = ANY(@exclude_projects::uuid[])))
  AND (cardinality(@statuses::text[]) = 0 OR status = ANY(@statuses::text[]))
  AND (cardinality(@priorities::smallint[]) = 0 OR priority = ANY(@priorities::smallint[]))
  AND (@tag::text = '' OR @tag::text = ANY(tags))
  AND (sqlc.narg(due_before)::timestamptz IS NULL OR due_at < sqlc.narg(due_before)::timestamptz)
  AND (sqlc.narg(due_after)::timestamptz IS NULL OR due_at >= sqlc.narg(due_after)::timestamptz)
  AND (sqlc.narg(overdue_at)::timestamptz IS NULL
       OR (due_at < sqlc.narg(overdue_at)::timestamptz AND status NOT IN ('done', 'cancelled')))
  AND (@search::text = '' OR title ILIKE @search::text OR notes ILIKE @search::text)
ORDER BY
  CASE WHEN @sort::text = 'priority' THEN priority END DESC,
  CASE WHEN @sort::text IN ('due', 'priority') THEN due_at END ASC NULLS LAST,
  CASE WHEN @sort::text = 'updated' THEN updated_at END DESC,
  created_at DESC,
  id DESC;

-- name: TodoProgress :many
SELECT project_id, status, count(*)::int AS total,
       (count(*) FILTER (WHERE due_at < @now AND status NOT IN ('done', 'cancelled')))::int AS overdue
FROM todos
WHERE user_id = @user_id
GROUP BY project_id, status;

-- name: DeleteTodosByProject :exec
DELETE FROM todos WHERE user_id = @user_id AND project_id = @project_id;

-- name: DeleteTodosByUser :exec
DELETE FROM todos WHERE user_id = @user_id;

-- projects

-- name: CreateProject :one
INSERT INTO projects (user_id, collection_id, name, description, icon, color, archived_at, created_at, updated_at)
VALUES (@user_id, sqlc.narg(collection_id), @name, @description, @icon, @color, sqlc.narg(archived_at), @created_at, @updated_at)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = @id AND user_id = @user_id;

-- name: UpdateProject :execrows
UPDATE projects
SET collection_id = sqlc.narg(collection_id), name = @name, description = @description, icon = @icon,
    color = @color, archived_at = sqlc.narg(archived_at), updated_at = @updated_at
WHERE id = @id AND user_id = @user_id;

-- name: DeleteProject :execrows
DELETE FROM projects WHERE id = @id AND user_id = @user_id;

-- archive: 0 active only, 1 archived only, 2 any (domain.ArchiveFilter).
-- name: ListProjects :many
SELECT * FROM projects
WHERE user_id = @user_id
  AND (sqlc.narg(collection_id)::uuid IS NULL OR collection_id = sqlc.narg(collection_id)::uuid)
  AND (@archive::int = 2 OR (archived_at IS NOT NULL) = (@archive::int = 1))
ORDER BY created_at, id;

-- name: DetachCollection :exec
UPDATE projects SET collection_id = NULL WHERE user_id = @user_id AND collection_id = @collection_id;

-- name: DeleteProjectsByUser :exec
DELETE FROM projects WHERE user_id = @user_id;

-- collections

-- name: CreateCollection :one
INSERT INTO collections (user_id, name, description, icon, color, archived_at, created_at, updated_at)
VALUES (@user_id, @name, @description, @icon, @color, sqlc.narg(archived_at), @created_at, @updated_at)
RETURNING *;

-- name: GetCollection :one
SELECT * FROM collections WHERE id = @id AND user_id = @user_id;

-- name: UpdateCollection :execrows
UPDATE collections
SET name = @name, description = @description, icon = @icon, color = @color,
    archived_at = sqlc.narg(archived_at), updated_at = @updated_at
WHERE id = @id AND user_id = @user_id;

-- name: DeleteCollection :execrows
DELETE FROM collections WHERE id = @id AND user_id = @user_id;

-- name: ListCollections :many
SELECT * FROM collections
WHERE user_id = @user_id
  AND (@archive::int = 2 OR (archived_at IS NOT NULL) = (@archive::int = 1))
ORDER BY created_at, id;

-- name: DeleteCollectionsByUser :exec
DELETE FROM collections WHERE user_id = @user_id;
