package command

import (
	"context"
	"log/slog"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/application"
)

const (
	// dueAlertBatch is how many todos one scan of the store returns.
	dueAlertBatch = 100
	// MaxDueAlertsPerRun caps the alerts one run sends; the rest go out on
	// the next run (the job runs every few minutes).
	MaxDueAlertsPerRun = 2000
)

// SendDueAlerts sends the due-date alerts that are due (domain.NextDueAlert):
// "due soon" for open todos due within 24 hours and "overdue" for open todos
// past their deadline, each once per deadline, to the todo's UserID (the
// owner of a personal todo, the creator of a team todo). Todos of archived
// projects get none. It runs as a system job, not as an actor.
type SendDueAlerts struct{}

type SendDueAlertsHandler struct{ base }

// Handle scans in batches until nothing is pending or MaxDueAlertsPerRun is
// reached. Each todo is marked and its push enqueued in one unit of work, so
// a crash never loses or repeats an alert; a marked todo leaves the scan, so
// the next batch starts after it.
func (h SendDueAlertsHandler) Handle(ctx context.Context, _ SendDueAlerts) error {
	now := h.now()
	processed, sent := 0, 0
	defer func() {
		if sent > 0 {
			slog.InfoContext(ctx, "due-date alerts sent", "count", sent)
		}
	}()
	for processed < MaxDueAlertsPerRun {
		limit := min(dueAlertBatch, MaxDueAlertsPerRun-processed)
		batch, err := h.Todos.ListPendingDueAlertsSystemWide(ctx, now, limit)
		if err != nil {
			return err
		}
		for _, a := range batch {
			ok, err := h.alert(ctx, a, now)
			if err != nil {
				return err
			}
			if ok {
				sent++
			}
		}
		processed += len(batch)
		if len(batch) < limit {
			return nil
		}
	}
	slog.WarnContext(ctx, "due-date alerts capped; the rest go out on the next run", "limit", MaxDueAlertsPerRun)
	return nil
}

// alert marks one todo and enqueues its push, together. It reports false
// when another run (or a change to the todo) got there first.
func (h SendDueAlertsHandler) alert(ctx context.Context, a application.DueAlert, now time.Time) (bool, error) {
	t := a.Todo
	if t.DueAt == nil {
		return false, nil
	}
	due := *t.DueAt
	sent := false
	err := h.Tx.WithinTx(ctx, func(ctx context.Context) error {
		marked, err := h.Todos.MarkDueAlerted(ctx, t.ID, a.Kind, due)
		if err != nil || !marked {
			return err
		}
		sent = true
		return h.Notifier.TodoDue(ctx, application.TodoDueAlert{
			UserID: t.UserID, TodoID: t.ID, Title: t.Title,
			TeamID: t.TeamID, ProjectID: t.ProjectID,
			Kind: a.Kind, DueAt: due,
		}, now)
	})
	return sent && err == nil, err
}
