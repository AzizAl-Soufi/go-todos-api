-- +goose Up
-- Due-date alerts (todos module): the deadline each alert was last sent for
-- (NULL: never). Remembering the deadline instead of a flag re-arms the
-- alerts when it changes.
ALTER TABLE todos
    ADD COLUMN IF NOT EXISTS due_soon_alerted_for TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS overdue_alerted_for  TIMESTAMPTZ;

-- The alert job's scan: open todos with a deadline whose overdue alert
-- hasn't gone out for it. Todos leave the index once alerted overdue, so the
-- scan stays proportional to the deadlines coming up, not to every overdue
-- todo nobody closed. The predicate must match ListPendingDueAlerts.
CREATE INDEX IF NOT EXISTS idx_todos_due_alerts ON todos (due_at, id)
    WHERE status NOT IN ('done', 'cancelled')
      AND due_at IS NOT NULL
      AND overdue_alerted_for IS DISTINCT FROM due_at;

-- +goose Down
DROP INDEX IF EXISTS idx_todos_due_alerts;
ALTER TABLE todos
    DROP COLUMN IF EXISTS overdue_alerted_for,
    DROP COLUMN IF EXISTS due_soon_alerted_for;
