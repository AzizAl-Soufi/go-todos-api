package apperrors

import "time"

// RetryLaterError is an AppError the client may retry from a known time on,
// such as a rate limit or a cooldown. It answers with the wrapped error's
// status, code and message; errors.Is still matches the wrapped error.
type RetryLaterError struct {
	AppError
	// At is when the client may try again: UTC, rounded up to the second.
	At time.Time
	// Wait is how long that is from the time the error was made.
	Wait time.Duration
}

var _ error = *new(RetryLaterError)


// RetryAt makes err retryable from at on. now is the caller's clock, so
// Wait stays consistent with At under a fake clock in tests.
func RetryAt(err AppError, now, at time.Time) RetryLaterError {
	at = at.UTC()
	if rounded := at.Truncate(time.Second); !rounded.Equal(at) {
		at = rounded.Add(time.Second)
	}
	return RetryLaterError{AppError: err, At: at, Wait: max(at.Sub(now), 0)}
}

func (e RetryLaterError) Unwrap() error { return e.AppError }
