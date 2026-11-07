package httpx

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

func TestChainOrder(t *testing.T) {
	var order []string
	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }), mw("outer"), mw("inner"))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if strings.Join(order, ",") != "outer,inner,handler" {
		t.Fatalf("order = %v", order)
	}
}

func TestRequestID(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = RequestIDFrom(r.Context()) }))

	run := func(incoming string) string {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if incoming != "" {
			req.Header.Set(RequestIDHeader, incoming)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get(RequestIDHeader) != seen {
			t.Fatalf("response header %q != context %q", rec.Header().Get(RequestIDHeader), seen)
		}
		return seen
	}

	if got := run("abc-123"); got != "abc-123" {
		t.Fatalf("well-formed incoming ID not reused: %q", got)
	}
	if got := run("bad id\nwith newline"); got == "" || strings.ContainsAny(got, " \n") {
		t.Fatalf("malformed incoming ID must be replaced, got %q", got)
	}
	if a, b := run(""), run(""); a == "" || a == b {
		t.Fatalf("generated IDs must be unique: %q %q", a, b)
	}
}

func TestAccessLog(t *testing.T) {
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }), RequestID, AccessLog)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/todos", nil))

	line := buf.String()
	for _, want := range []string{"method=POST", "path=/todos", "status=418", "request_id="} {
		if !strings.Contains(line, want) {
			t.Errorf("access log missing %q: %s", want, line)
		}
	}
}

func TestDecodeJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	decode := func(body string) (payload, error) {
		var p payload
		err := DecodeJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), &p)
		return p, err
	}

	if p, err := decode(`{"name":"x"}`); err != nil || p.Name != "x" {
		t.Fatalf("valid body: %+v, %v", p, err)
	}
	for _, bad := range []string{``, `{`, `{"name":1}`, `{"name":"x"} {"name":"y"}`} {
		if _, err := decode(bad); !errors.Is(err, apperrors.ErrInvalidRequestBody) {
			t.Errorf("decode(%q) err = %v, want ErrInvalidRequestBody", bad, err)
		}
	}
}

func TestRespondNoContent(t *testing.T) {
	rec := httptest.NewRecorder()
	RespondNoContent(rec)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status %d body %q", rec.Code, rec.Body)
	}
}
