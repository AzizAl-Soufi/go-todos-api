// Package pgstore is the PostgreSQL job store. Each call runs on the
// transaction carried by ctx when there is one (so enqueueing joins the
// caller's unit of work), otherwise on the pool.
package pgstore

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/database/postgres"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs/pgstore/sqlcgen"
)

type Store struct{ pool *pgxpool.Pool }

var _ jobs.Store = (*Store)(nil)

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Executor(ctx, s.pool))
}

func toJob(r sqlcgen.Job) *jobs.Job {
	j := &jobs.Job{
		ID: r.ID.String(), Kind: r.Kind, Payload: r.Payload, OwnerID: r.OwnerID, Status: jobs.Status(r.Status),
		Attempts: int(r.Attempts), MaxAttempts: int(r.MaxAttempts), RunAt: r.RunAt.UTC(), LastError: r.LastError,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
	if r.UniqueKey != nil {
		j.UniqueKey = *r.UniqueKey
	}
	if r.LockedUntil != nil {
		j.LockedUntil = r.LockedUntil.UTC()
	}
	if r.FinishedAt != nil {
		j.FinishedAt = r.FinishedAt.UTC()
	}
	return j
}

func (s *Store) Enqueue(ctx context.Context, j *jobs.Job) error {
	var key *string
	if j.UniqueKey != "" {
		key = &j.UniqueKey
	}
	q := s.q(ctx)
	id, err := q.InsertJob(ctx, sqlcgen.InsertJobParams{
		Kind: j.Kind, Payload: j.Payload, UniqueKey: key, OwnerID: j.OwnerID, MaxAttempts: int32(j.MaxAttempts),
		RunAt: j.RunAt, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	})
	if errors.Is(err, pgx.ErrNoRows) && key != nil {
		id, err = q.GetJobIDByUniqueKey(ctx, key)
	}
	if err != nil {
		return postgres.MapErr("enqueue job", err)
	}
	j.ID = id.String()
	return nil
}

func (s *Store) Claim(ctx context.Context, c jobs.Claim) ([]*jobs.Job, error) {
	add := int32(0)
	if c.CountAttempt {
		add = 1
	}
	until := c.Now.Add(c.Lease)
	rows, err := s.q(ctx).ClaimJobs(ctx, sqlcgen.ClaimJobsParams{AddAttempts: add, LockedUntil: &until, Now: c.Now, Lim: int32(c.Limit)})
	if err != nil {
		return nil, postgres.MapErr("claim jobs", err)
	}
	out := make([]*jobs.Job, 0, len(rows))
	for _, r := range rows {
		out = append(out, toJob(r))
	}
	slices.SortFunc(out, func(a, b *jobs.Job) int { return cmp.Or(a.RunAt.Compare(b.RunAt), cmp.Compare(a.ID, b.ID)) })
	return out, nil
}

func (s *Store) Succeed(ctx context.Context, id string, at time.Time) error {
	u, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).SucceedJob(ctx, sqlcgen.SucceedJobParams{ID: u, At: &at})
	return postgres.RequireAffected("succeed job", rows, err)
}

func (s *Store) Fail(ctx context.Context, id string, at time.Time, errMsg string) error {
	u, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).FailJob(ctx, sqlcgen.FailJobParams{ID: u, At: &at, LastError: errMsg})
	return postgres.RequireAffected("fail job", rows, err)
}

func (s *Store) Retry(ctx context.Context, id string, runAt, at time.Time, errMsg string) error {
	u, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).RetryJob(ctx, sqlcgen.RetryJobParams{ID: u, RunAt: runAt, At: at, LastError: errMsg})
	return postgres.RequireAffected("retry job", rows, err)
}

func (s *Store) Dispatch(ctx context.Context, id string, at time.Time) error {
	u, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).DispatchJob(ctx, sqlcgen.DispatchJobParams{ID: u, At: at})
	return postgres.RequireAffected("dispatch job", rows, err)
}

func (s *Store) Attempted(ctx context.Context, id string, attempts int, at time.Time, errMsg string) error {
	u, err := ids.UUID(id)
	if err != nil {
		return err
	}
	rows, err := s.q(ctx).RecordJobAttempt(ctx, sqlcgen.RecordJobAttemptParams{
		ID: u, Attempts: int32(attempts), At: at, LastError: errMsg,
	})
	return postgres.RequireAffected("record job attempt", rows, err)
}

func (s *Store) Get(ctx context.Context, id string) (*jobs.Job, error) {
	u, err := ids.UUID(id)
	if err != nil {
		return nil, err
	}
	row, err := s.q(ctx).GetJob(ctx, u)
	if err != nil {
		return nil, postgres.MapErr("get job", err)
	}
	return toJob(row), nil
}

func (s *Store) DeleteFinished(ctx context.Context, before time.Time) (int64, error) {
	n, err := s.q(ctx).DeleteFinishedJobs(ctx, &before)
	return n, postgres.MapErr("delete finished jobs", err)
}

func (s *Store) Counts(ctx context.Context, since time.Time) (jobs.Counts, error) {
	q := s.q(ctx)
	rows, err := q.CountJobsByStatus(ctx)
	if err != nil {
		return jobs.Counts{}, postgres.MapErr("count jobs", err)
	}
	c := jobs.Counts{ByStatus: map[jobs.Status]int64{}}
	for _, r := range rows {
		c.ByStatus[jobs.Status(r.Status)] = r.N
	}
	if c.FailedSince, err = q.CountJobsFailedSince(ctx, &since); err != nil {
		return jobs.Counts{}, postgres.MapErr("count failed jobs", err)
	}
	oldest, err := q.OldestQueuedJob(ctx)
	switch {
	case err == nil:
		c.OldestQueued = oldest.UTC()
	case !errors.Is(err, pgx.ErrNoRows):
		return jobs.Counts{}, postgres.MapErr("oldest queued job", err)
	}
	return c, nil
}

func (s *Store) AcquireLease(ctx context.Context, name, holder string, until, now time.Time) (bool, error) {
	_, err := s.q(ctx).AcquireLease(ctx, sqlcgen.AcquireLeaseParams{Name: name, Holder: holder, Until: until, Now: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, postgres.MapErr("acquire lease", err)
}

func (s *Store) ReleaseLease(ctx context.Context, name, holder string) error {
	return postgres.MapErr("release lease", s.q(ctx).ReleaseLease(ctx, sqlcgen.ReleaseLeaseParams{Name: name, Holder: holder}))
}
