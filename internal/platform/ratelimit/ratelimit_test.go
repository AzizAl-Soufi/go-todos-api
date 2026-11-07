package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newTestLimiter(perMinute, burst int) (*Limiter, *fakeClock) {
	c := &fakeClock{t: time.Unix(0, 0)}
	l := New(perMinute, burst)
	l.now = c.now
	return l, c
}

func TestAllow_BurstThenRefill(t *testing.T) {
	l, clock := newTestLimiter(60, 3) // one token per second

	for i := range 3 {
		if ok, _ := l.Allow("ip"); !ok {
			t.Fatalf("request %d within burst was rejected", i)
		}
	}
	ok, wait := l.Allow("ip")
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("over burst: ok=%v wait=%v, want rejected with wait <= 1s", ok, wait)
	}

	clock.t = clock.t.Add(time.Second)
	if ok, _ := l.Allow("ip"); !ok {
		t.Fatal("a token should have refilled after 1s")
	}
}

func TestAllow_KeysAreIndependent(t *testing.T) {
	l, _ := newTestLimiter(60, 1)
	l.Allow("a")
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("one client's usage must not affect another")
	}
}

func TestSweepRemovesIdleBuckets(t *testing.T) {
	l, clock := newTestLimiter(60, 1)
	l.Allow("old")
	clock.t = clock.t.Add(idleTTL + time.Minute)
	l.Allow("new")
	if _, ok := l.buckets["old"]; ok {
		t.Fatal("idle bucket was not swept")
	}
}

func TestMiddleware(t *testing.T) {
	l, _ := newTestLimiter(60, 1)
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	do := func(remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.RemoteAddr = remote
		req.Header.Set("X-Forwarded-For", "1.2.3.4") // must be ignored
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := do("10.0.0.1:1111"); rec.Code != http.StatusNoContent {
		t.Fatalf("first request: %d", rec.Code)
	}
	rec := do("10.0.0.1:2222") // same IP, different port
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("second request: %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := do("10.0.0.2:1111"); rec.Code != http.StatusNoContent {
		t.Fatalf("other IP: %d", rec.Code)
	}
}
