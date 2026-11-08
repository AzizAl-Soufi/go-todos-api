-- Queries of the job store (platform/jobs). Regenerate with `make sqlc`.

-- A unique_key used before: no row, and the caller looks the job up.
-- name: InsertJob :one
INSERT INTO jobs (kind, payload, unique_key, owner_id, status, attempts, max_attempts, run_at, created_at, updated_at)
VALUES (@kind, @payload, sqlc.narg('unique_key'), @owner_id, 'queued', 0, @max_attempts, @run_at, @created_at, @updated_at)
ON CONFLICT (unique_key) DO NOTHING
RETURNING id;

-- name: GetJobIDByUniqueKey :one
SELECT id FROM jobs WHERE unique_key = @unique_key;

-- Several workers claim at once without blocking each other: each skips the
-- rows another has locked.
-- name: ClaimJobs :many
UPDATE jobs
SET status = 'running', attempts = attempts + @add_attempts::int, locked_until = @locked_until, updated_at = @now
WHERE id IN (
    SELECT id FROM jobs
    WHERE (status = 'queued' AND run_at <= @now) OR (status = 'running' AND locked_until < @now)
    ORDER BY run_at, id
    LIMIT @lim
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: SucceedJob :execrows
UPDATE jobs SET status = 'succeeded', locked_until = NULL, finished_at = @at, updated_at = @at WHERE id = @id;

-- name: FailJob :execrows
UPDATE jobs SET status = 'failed', locked_until = NULL, last_error = @last_error, finished_at = @at, updated_at = @at
WHERE id = @id;

-- name: RetryJob :execrows
UPDATE jobs SET status = 'queued', locked_until = NULL, run_at = @run_at, last_error = @last_error, updated_at = @at
WHERE id = @id;

-- name: DispatchJob :execrows
UPDATE jobs SET status = 'dispatched', locked_until = NULL, updated_at = @at WHERE id = @id;

-- name: RecordJobAttempt :execrows
UPDATE jobs SET attempts = @attempts, last_error = @last_error, updated_at = @at WHERE id = @id;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = @id;

-- name: DeleteFinishedJobs :execrows
DELETE FROM jobs WHERE finished_at < @before;

-- name: CountJobsByStatus :many
SELECT status, COUNT(*)::bigint AS n FROM jobs GROUP BY status;

-- name: CountJobsFailedSince :one
SELECT COUNT(*)::bigint FROM jobs WHERE status = 'failed' AND finished_at >= @since;

-- name: OldestQueuedJob :one
SELECT run_at FROM jobs WHERE status = 'queued' ORDER BY run_at LIMIT 1;

-- Taken when free, expired, or already ours; otherwise no row.
-- name: AcquireLease :one
INSERT INTO job_leases (name, holder, expires_at) VALUES (@name, @holder, @until)
ON CONFLICT (name) DO UPDATE SET holder = EXCLUDED.holder, expires_at = EXCLUDED.expires_at
WHERE job_leases.expires_at < @now OR job_leases.holder = EXCLUDED.holder
RETURNING holder;

-- name: ReleaseLease :exec
DELETE FROM job_leases WHERE name = @name AND holder = @holder;
