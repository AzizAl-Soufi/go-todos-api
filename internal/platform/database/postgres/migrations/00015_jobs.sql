-- +goose Up
-- Background jobs (platform/jobs): the transactional outbox and job queue,
-- and the leases that elect one scheduler among the worker processes.
-- unique_key dedupes events and schedule slots (NULLs never clash).
CREATE TABLE IF NOT EXISTS jobs (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         TEXT        NOT NULL,
    payload      JSONB       NOT NULL,
    unique_key   TEXT        UNIQUE,
    owner_id     TEXT        NOT NULL DEFAULT '',
    status       TEXT        NOT NULL CHECK (status IN ('queued', 'running', 'dispatched', 'succeeded', 'failed')),
    attempts     INTEGER     NOT NULL DEFAULT 0,
    max_attempts INTEGER     NOT NULL,
    run_at       TIMESTAMPTZ NOT NULL,
    locked_until TIMESTAMPTZ,
    last_error   TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at  TIMESTAMPTZ
);
-- Claiming looks for due queued jobs and expired leases.
CREATE INDEX IF NOT EXISTS idx_jobs_queued_run_at       ON jobs (run_at)       WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS idx_jobs_running_locked_until ON jobs (locked_until) WHERE status = 'running';
CREATE INDEX IF NOT EXISTS idx_jobs_finished_at          ON jobs (finished_at)  WHERE finished_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS job_leases (
    name       TEXT        PRIMARY KEY,
    holder     TEXT        NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS job_leases;
DROP TABLE IF EXISTS jobs;
