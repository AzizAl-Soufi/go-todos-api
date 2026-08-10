-- users
-- name: CreateUser :one
INSERT INTO users (name, email)
VALUES ($1, $2)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1 LIMIT 1;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;


-- todos
-- name: CreateTodo :one
INSERT INTO todos (user_id, title)
VALUES ($1, $2)
RETURNING *;

-- name: GetTodo :one
SELECT * FROM todos WHERE id = $1 AND user_id = $2;

-- name: UpdateTodo :one
UPDATE todos 
SET title = COALESCE($1, title),
	completed = COALESCE($2, completed)
WHERE id = $3 AND user_id = $4
RETURNING *;

-- name: DeleteTodo :execrows
DELETE FROM todos WHERE id = $1 AND user_id = $2;

-- name: GetTodosByUser :many
SELECT * FROM todos 
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: DeleteTodosByUser :execrows
DELETE FROM todos
WHERE user_id = $1;