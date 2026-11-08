// Package cimd fetches Client ID Metadata Documents: an OAuth client (such
// as Claude Code) identifies itself with an HTTPS URL that serves its
// metadata (application.ClientMetadataFetcher).
//
// The URL comes from whoever starts an authorization, so fetching it is a
// server-side request forgery risk. The fetcher only talks HTTPS to public
// addresses (checked on the resolved IP, so DNS rebinding can't reach the
// internal network), follows no redirects, and bounds time and size.
package cimd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
)

const (
	fetchTimeout = 5 * time.Second
	maxDocBytes  = 64 << 10
	cacheTTL     = 5 * time.Minute
	maxCached    = 1000
)

type entry struct {
	client  domain.Client
	fetched time.Time
}

type Fetcher struct {
	client *http.Client
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]entry
}

var _ application.ClientMetadataFetcher = (*Fetcher)(nil)

// New returns a fetcher that only reaches public internet addresses.
func New() *Fetcher {
	dialer := &net.Dialer{Timeout: fetchTimeout, Control: publicOnly}
	transport := &http.Transport{
		Proxy:                 nil, // a proxy would dial for us, past publicOnly
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   fetchTimeout,
		ResponseHeaderTimeout: fetchTimeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       time.Minute,
	}
	return newFetcher(&http.Client{Transport: transport})
}

func newFetcher(c *http.Client) *Fetcher {
	c.Timeout = fetchTimeout
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Fetcher{client: c, now: time.Now, cache: map[string]entry{}}
}

// errNotPublic is a dial to an address that isn't on the public internet.
var errNotPublic = errors.New("cimd: address is not public")

// publicOnly runs after DNS resolution, on the address actually dialed.
func publicOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsUnspecified() || isSharedAddress(ip) {
		return errNotPublic
	}
	return nil
}

// isSharedAddress is 100.64.0.0/10 (carrier-grade NAT, RFC 6598), which
// IsPrivate doesn't cover.
func isSharedAddress(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xC0 == 64
}

// document is the part of a client metadata document the server uses.
type document struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

func (f *Fetcher) Fetch(ctx context.Context, clientID string) (*domain.Client, error) {
	if c, ok := f.cached(clientID); ok {
		return c, nil
	}
	client, err := f.fetch(ctx, clientID)
	if err != nil {
		slog.Warn("client metadata document rejected", "client_id", clientID, "error", err)
		return nil, domain.ErrInvalidClient
	}
	f.store(clientID, client)
	return client, nil
}

func (f *Fetcher) fetch(ctx context.Context, clientID string) (*domain.Client, error) {
	if err := validateClientIDURL(clientID); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxDocBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxDocBytes {
		return nil, errors.New("document too large")
	}
	var doc document
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	switch {
	case doc.ClientID != clientID:
		return nil, errors.New("client_id does not match the document URL")
	case doc.TokenEndpointAuthMethod != "" && doc.TokenEndpointAuthMethod != "none":
		return nil, errors.New("only public clients (token_endpoint_auth_method none) are supported")
	}
	client, err := domain.NewClient(doc.ClientName, doc.RedirectURIs, domain.MetadataDocument, f.now())
	if err != nil {
		return nil, err
	}
	client.ID = clientID
	return client, nil
}

// validateClientIDURL checks the client ID is an HTTPS URL with a path, as
// the CIMD specification requires.
func validateClientIDURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	switch {
	case u.Scheme != "https", u.Host == "", u.User != nil, u.Fragment != "":
		return errors.New("client_id must be an https URL without credentials or fragment")
	case u.Path == "" || u.Path == "/":
		return errors.New("client_id URL must have a path")
	case strings.Contains(u.Path, "/./") || strings.Contains(u.Path, "/../"):
		return errors.New("client_id URL must not have dot segments")
	}
	return nil
}

func (f *Fetcher) cached(clientID string) (*domain.Client, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.cache[clientID]
	if !ok || f.now().Sub(e.fetched) > cacheTTL {
		return nil, false
	}
	c := e.client
	return &c, true
}

func (f *Fetcher) store(clientID string, c *domain.Client) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cache) >= maxCached {
		clear(f.cache) // crude, but bounded; refetching is cheap
	}
	f.cache[clientID] = entry{client: *c, fetched: f.now()}
}
