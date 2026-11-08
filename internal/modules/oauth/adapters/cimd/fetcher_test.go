package cimd

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
)

// metadataServer serves docs[path] as a client metadata document whose
// client_id is the document's own URL (unless the doc overrides it).
func metadataServer(t *testing.T, docs map[string]map[string]any) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		doc, ok := docs[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, set := doc["client_id"]; !set {
			doc["client_id"] = srv.URL + r.URL.Path
		}
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestFetch(t *testing.T) {
	srv, hits := metadataServer(t, map[string]map[string]any{
		"/claude":        {"client_name": "Claude Code", "redirect_uris": []string{"http://localhost/callback", "http://127.0.0.1/callback"}},
		"/other-id":      {"client_id": "https://evil.example/claude", "redirect_uris": []string{"https://evil.example/cb"}},
		"/secret":        {"redirect_uris": []string{"https://app.example/cb"}, "token_endpoint_auth_method": "client_secret_basic"},
		"/no-redirects":  {"client_name": "x"},
		"/http-redirect": {"redirect_uris": []string{"http://app.example/cb"}},
	})
	f := newFetcher(srv.Client())
	ctx := context.Background()

	id := srv.URL + "/claude"
	c, err := f.Fetch(ctx, id)
	if err != nil || c.ID != id || c.Name != "Claude Code" || c.Kind != domain.MetadataDocument || !c.AllowsRedirect("http://localhost:3118/callback") {
		t.Fatalf("Fetch = %+v (%v)", c, err)
	}
	if _, err := f.Fetch(ctx, id); err != nil || hits.Load() != 1 {
		t.Fatalf("second fetch: %v, %d requests; want it cached", err, hits.Load())
	}

	for _, bad := range []string{
		srv.URL + "/other-id",      // client_id isn't the document's URL
		srv.URL + "/secret",        // confidential clients aren't supported
		srv.URL + "/no-redirects",  // no redirect URIs
		srv.URL + "/http-redirect", // http redirect to a non-loopback host
		srv.URL + "/missing",       // 404
		srv.URL,                    // no path
		strings.Replace(srv.URL, "https://", "http://", 1) + "/claude",
	} {
		if _, err := f.Fetch(ctx, bad); !errors.Is(err, domain.ErrInvalidClient) {
			t.Errorf("Fetch(%q): err = %v, want ErrInvalidClient", bad, err)
		}
	}
}

func TestNew_RefusesInternalAddresses(t *testing.T) {
	srv, hits := metadataServer(t, map[string]map[string]any{
		"/claude": {"redirect_uris": []string{"http://localhost/callback"}},
	})
	// The production fetcher won't dial the loopback test server.
	if _, err := New().Fetch(context.Background(), srv.URL+"/claude"); !errors.Is(err, domain.ErrInvalidClient) {
		t.Fatalf("err = %v, want ErrInvalidClient", err)
	}
	if hits.Load() != 0 {
		t.Fatal("the request reached a loopback server")
	}
}

func TestPublicOnly(t *testing.T) {
	for addr, public := range map[string]bool{
		"93.184.216.34:443":   true,
		"[2606:4700::1]:443":  true,
		"127.0.0.1:443":       false,
		"10.0.0.5:443":        false,
		"172.16.0.1:443":      false,
		"192.168.1.1:443":     false,
		"169.254.169.254:443": false, // cloud metadata
		"100.64.0.1:443":      false,
		"0.0.0.0:443":         false,
		"[::1]:443":           false,
		"[fc00::1]:443":       false,
		"[fe80::1]:443":       false,
	} {
		err := publicOnly("tcp", addr, nil)
		if (err == nil) != public {
			t.Errorf("publicOnly(%s) = %v, want public %v", addr, err, public)
		}
	}
	if _, _, err := net.SplitHostPort("nope"); err == nil {
		t.Fatal("sanity")
	}
}
