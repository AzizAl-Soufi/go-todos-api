// Package ratelimit provides an in-process, per-key token-bucket limiter.
//
// State lives in memory, so limits apply per instance; behind several replicas
// use a shared store or rate-limit at the load balancer instead.
package ratelimit

import (
	"math"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/httpx"
)

var ErrTooManyRequests = apperrors.TooManyRequests("RATE_LIMITED", "too many requests, slow down")

// idleTTL is how long an unused bucket is kept before it is swept.
const idleTTL = 10 * time.Minute

type bucket struct {
	tokens float64
	last   time.Time
}

type Limiter struct {
	ratePerSec float64
	burst      float64
	now        func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

// New allows perMinute requests per key, with bursts of up to burst requests.
func New(perMinute, burst int) *Limiter {
	return &Limiter{
		ratePerSec: float64(perMinute) / 60,
		burst:      float64(max(burst, 1)),
		now:        time.Now,
		buckets:    make(map[string]*bucket),
	}
}

// Allow consumes one token for key. When the bucket is empty it returns false
// and how long until the next token is available.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.sweep(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}

	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.ratePerSec)
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	if l.ratePerSec <= 0 {
		return false, time.Minute
	}
	wait := time.Duration((1 - b.tokens) / l.ratePerSec * float64(time.Second))
	return false, wait
}

func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < idleTTL {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > idleTTL {
			delete(l.buckets, k)
		}
	}
}

// Middleware limits requests per client IP and answers 429 with Retry-After.
// The IP comes from RemoteAddr; X-Forwarded-For is deliberately not trusted.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return l.MiddlewareBy(clientIP)(next)
}

// MiddlewareBy limits requests per key(r), e.g. the signed-in user, and
// answers 429 with Retry-After.
func (l *Limiter) MiddlewareBy(key func(*http.Request) string) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return l.limit(key, next)
	}
}

func (l *Limiter) limit(key func(*http.Request) string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := l.Check(key(r)); err != nil {
			httpx.RespondErr(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Check takes one request from key's budget: nil, or ErrTooManyRequests
// retryable from when the next request fits. For limits outside HTTP
// middleware (an MCP tool shares POST /todos's budget).
func (l *Limiter) Check(key string) error {
	ok, wait := l.Allow(key)
	if ok {
		return nil
	}
	now := l.now()
	return apperrors.RetryAt(ErrTooManyRequests, now, now.Add(wait))
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
