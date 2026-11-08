// Package memstore is the in-memory job store (local development, tests).
// It isn't shared between processes: with DB_TYPE=memory, run the workers
// inside the API process.
package memstore

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/ids"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/jobs"
)

type lease struct {
	holder  string
	expires time.Time
}

type Store struct {
	mu     sync.Mutex
	jobs   map[string]jobs.Job
	keys   map[string]string // unique key -> job ID
	leases map[string]lease
}

var _ jobs.Store = (*Store)(nil)

func New() *Store {
	return &Store{jobs: map[string]jobs.Job{}, keys: map[string]string{}, leases: map[string]lease{}}
}

func clone(j jobs.Job) *jobs.Job {
	j.Payload = slices.Clone(j.Payload)
	return &j
}

func (s *Store) Enqueue(_ context.Context, j *jobs.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j.UniqueKey != "" {
		if id, ok := s.keys[j.UniqueKey]; ok {
			j.ID = id
			return nil
		}
	}
	j.ID = bson.NewObjectID().Hex()
	s.jobs[j.ID] = *clone(*j)
	if j.UniqueKey != "" {
		s.keys[j.UniqueKey] = j.ID
	}
	return nil
}

func (s *Store) Claim(_ context.Context, c jobs.Claim) ([]*jobs.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var due []jobs.Job
	for _, j := range s.jobs {
		queued := j.Status == jobs.Queued && !j.RunAt.After(c.Now)
		abandoned := j.Status == jobs.Running && j.LockedUntil.Before(c.Now)
		if queued || abandoned {
			due = append(due, j)
		}
	}
	slices.SortFunc(due, func(a, b jobs.Job) int { return cmp.Or(a.RunAt.Compare(b.RunAt), cmp.Compare(a.ID, b.ID)) })
	out := []*jobs.Job{}
	for _, j := range due[:min(c.Limit, len(due))] {
		j.Status = jobs.Running
		j.LockedUntil = c.Now.Add(c.Lease)
		j.UpdatedAt = c.Now
		if c.CountAttempt {
			j.Attempts++
		}
		s.jobs[j.ID] = j
		out = append(out, clone(j))
	}
	return out, nil
}

func (s *Store) update(id string, fn func(*jobs.Job)) error {
	if _, err := ids.ObjectID(id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return apperrors.ErrNotFound
	}
	fn(&j)
	s.jobs[id] = j
	return nil
}

func (s *Store) Succeed(_ context.Context, id string, at time.Time) error {
	return s.update(id, func(j *jobs.Job) {
		j.Status, j.LockedUntil, j.FinishedAt, j.UpdatedAt = jobs.Succeeded, time.Time{}, at, at
	})
}

func (s *Store) Fail(_ context.Context, id string, at time.Time, errMsg string) error {
	return s.update(id, func(j *jobs.Job) {
		j.Status, j.LockedUntil, j.FinishedAt, j.UpdatedAt, j.LastError = jobs.Failed, time.Time{}, at, at, errMsg
	})
}

func (s *Store) Retry(_ context.Context, id string, runAt, at time.Time, errMsg string) error {
	return s.update(id, func(j *jobs.Job) {
		j.Status, j.LockedUntil, j.RunAt, j.UpdatedAt, j.LastError = jobs.Queued, time.Time{}, runAt, at, errMsg
	})
}

func (s *Store) Dispatch(_ context.Context, id string, at time.Time) error {
	return s.update(id, func(j *jobs.Job) {
		j.Status, j.LockedUntil, j.UpdatedAt = jobs.Dispatched, time.Time{}, at
	})
}

func (s *Store) Attempted(_ context.Context, id string, attempts int, at time.Time, errMsg string) error {
	return s.update(id, func(j *jobs.Job) {
		j.Attempts, j.UpdatedAt, j.LastError = attempts, at, errMsg
	})
}

func (s *Store) Get(_ context.Context, id string) (*jobs.Job, error) {
	if _, err := ids.ObjectID(id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, apperrors.ErrNotFound
	}
	return clone(j), nil
}

func (s *Store) DeleteFinished(_ context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for id, j := range s.jobs {
		if j.Status.Finished() && j.FinishedAt.Before(before) {
			delete(s.jobs, id)
			if j.UniqueKey != "" {
				delete(s.keys, j.UniqueKey)
			}
			n++
		}
	}
	return n, nil
}

func (s *Store) Counts(_ context.Context, since time.Time) (jobs.Counts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := jobs.Counts{ByStatus: map[jobs.Status]int64{}}
	for _, j := range s.jobs {
		c.ByStatus[j.Status]++
		if j.Status == jobs.Failed && !j.FinishedAt.Before(since) {
			c.FailedSince++
		}
		if j.Status == jobs.Queued && (c.OldestQueued.IsZero() || j.RunAt.Before(c.OldestQueued)) {
			c.OldestQueued = j.RunAt
		}
	}
	return c, nil
}

func (s *Store) AcquireLease(_ context.Context, name, holder string, until, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.leases[name]; ok && l.holder != holder && !l.expires.Before(now) {
		return false, nil
	}
	s.leases[name] = lease{holder: holder, expires: until}
	return true, nil
}

func (s *Store) ReleaseLease(_ context.Context, name, holder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.leases[name]; ok && l.holder == holder {
		delete(s.leases, name)
	}
	return nil
}
