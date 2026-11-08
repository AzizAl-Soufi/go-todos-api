package httpapi

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/oauth/domain"
)

// SignIn is the Firebase web configuration the sign-in page uses (Google,
// email and password). APIKey is the public web API key, not a secret.
type SignIn struct {
	APIKey     string
	AuthDomain string // default <project>.firebaseapp.com
	ProjectID  string
}

//go:embed templates/authorize.html
var templates embed.FS

type signInPage struct {
	tmpl *template.Template
	cfg  SignIn
}

func newSignInPage(cfg SignIn) *signInPage {
	if cfg.AuthDomain == "" && cfg.ProjectID != "" {
		cfg.AuthDomain = cfg.ProjectID + ".firebaseapp.com"
	}
	return &signInPage{tmpl: template.Must(template.ParseFS(templates, "templates/authorize.html")), cfg: cfg}
}

type pageData struct {
	Nonce          string
	Error          string
	ClientName     string
	RedirectHost   string
	LoopbackOnly   bool
	Params         map[string]string
	FirebaseConfig map[string]string
}

// render shows the sign-in and consent page for a checked request. The
// request's parameters travel in hidden fields and are checked again when
// the form comes back.
func (p *signInPage) render(w http.ResponseWriter, r *http.Request, client *domain.Client, req domain.AuthorizationRequest) {
	redirect, err := url.Parse(req.RedirectURI)
	if err != nil {
		p.error(w, r, http.StatusBadRequest, "The app's redirect address is invalid.")
		return
	}
	nonce := newNonce()
	p.headers(w, nonce, redirect.Scheme+"://"+redirect.Host)
	params := map[string]string{
		"response_type":         req.ResponseType,
		"client_id":             req.ClientID,
		"redirect_uri":          req.RedirectURI,
		"state":                 req.State,
		"code_challenge":        req.CodeChallenge,
		"code_challenge_method": req.CodeChallengeMethod,
		"resource":              req.Resource,
		"scope":                 req.Scope,
	}
	p.write(w, http.StatusOK, pageData{
		Nonce:        nonce,
		ClientName:   client.Name,
		RedirectHost: redirect.Host,
		LoopbackOnly: client.OnlyLoopback(),
		Params:       params,
		FirebaseConfig: map[string]string{
			"apiKey":     p.cfg.APIKey,
			"authDomain": p.cfg.AuthDomain,
			"projectId":  p.cfg.ProjectID,
		},
	})
}

// error shows a failure that can't go back to the client.
func (p *signInPage) error(w http.ResponseWriter, _ *http.Request, status int, msg string) {
	nonce := newNonce()
	p.headers(w, nonce, "")
	p.write(w, status, pageData{Nonce: nonce, Error: msg})
}

func (p *signInPage) write(w http.ResponseWriter, status int, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := p.tmpl.Execute(w, data); err != nil {
		slog.Error("render sign-in page", "error", err)
	}
}

// headers lock the page down: no framing (clickjacking a consent screen),
// scripts and styles only with this response's nonce or from Firebase, and
// the form may only post here and redirect to the client.
func (p *signInPage) headers(w http.ResponseWriter, nonce, redirectOrigin string) {
	h := w.Header()
	authDomain := "https://" + p.cfg.AuthDomain
	csp := []string{
		"default-src 'none'",
		"script-src 'nonce-" + nonce + "' https://www.gstatic.com https://apis.google.com",
		"style-src 'nonce-" + nonce + "'",
		"connect-src 'self' https://identitytoolkit.googleapis.com https://securetoken.googleapis.com https://www.googleapis.com https://apis.google.com",
		"frame-src " + authDomain + " https://accounts.google.com https://apis.google.com",
		"img-src 'self' data: https:",
		"form-action 'self' " + redirectOrigin,
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}
	h.Set("Content-Security-Policy", strings.Join(csp, "; "))
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
}

func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand never fails on supported platforms
	return base64.RawStdEncoding.EncodeToString(b)
}
