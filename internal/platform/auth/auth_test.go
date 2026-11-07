package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

type fakeAuthenticator struct {
	gotToken string
	p        Principal
	err      error
}

func (f *fakeAuthenticator) Authenticate(_ context.Context, token string) (Principal, error) {
	f.gotToken = token
	return f.p, f.err
}

func serve(h http.Handler, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func code(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Error.Code
}

func TestMiddleware(t *testing.T) {
	var seen Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = PrincipalFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	t.Run("valid token", func(t *testing.T) {
		a := &fakeAuthenticator{p: Principal{UserID: "u1"}}
		rec := serve(Middleware(a)(next), "bearer  abc ")
		if rec.Code != http.StatusNoContent || a.gotToken != "abc" || seen.UserID != "u1" {
			t.Fatalf("status %d, token %q, principal %+v", rec.Code, a.gotToken, seen)
		}
	})

	for name, header := range map[string]string{"missing": "", "basic scheme": "Basic abc", "empty token": "Bearer   "} {
		t.Run(name, func(t *testing.T) {
			rec := serve(Middleware(&fakeAuthenticator{})(next), header)
			if rec.Code != http.StatusUnauthorized || code(t, rec) != "MISSING_ACCESS_TOKEN" {
				t.Fatalf("status %d code %q", rec.Code, code(t, rec))
			}
		})
	}

	t.Run("authenticator error", func(t *testing.T) {
		a := &fakeAuthenticator{err: apperrors.Unauthorized("TOKEN_REVOKED", "revoked")}
		rec := serve(Middleware(a)(next), "Bearer abc")
		if rec.Code != http.StatusUnauthorized || code(t, rec) != "TOKEN_REVOKED" {
			t.Fatalf("status %d code %q", rec.Code, code(t, rec))
		}
	})
}

func TestRequireVerifiedEmail(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	run := func(ctx context.Context) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		RequireVerifiedEmail(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
		return rec
	}

	if rec := run(context.Background()); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no principal: status %d", rec.Code)
	}
	if rec := run(WithPrincipal(context.Background(), Principal{UserID: "u"})); rec.Code != http.StatusForbidden || code(t, rec) != "EMAIL_NOT_VERIFIED" {
		t.Fatalf("unverified: status %d", rec.Code)
	}
	if rec := run(WithPrincipal(context.Background(), Principal{UserID: "u", EmailVerified: true})); rec.Code != http.StatusNoContent {
		t.Fatalf("verified: status %d", rec.Code)
	}
}

func TestPrincipalFrom(t *testing.T) {
	if _, ok := PrincipalFrom(context.Background()); ok {
		t.Fatal("empty context has no principal")
	}
	if _, ok := PrincipalFrom(WithPrincipal(context.Background(), Principal{})); ok {
		t.Fatal("a principal without a user ID is not authenticated")
	}
}

func TestForUser(t *testing.T) {
	h := ForUser(func(r *http.Request, userID string) (int, any, error) {
		return http.StatusOK, map[string]string{"user": userID}, nil
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusUnauthorized || code(t, rec) != "UNAUTHORIZED" {
		t.Fatalf("no principal: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(WithPrincipal(context.Background(), Principal{UserID: "u1"})))
	if rec.Code != http.StatusOK || rec.Body.String() != `{"user":"u1"}`+"\n" {
		t.Fatalf("with principal: %d %s", rec.Code, rec.Body.String())
	}
}
