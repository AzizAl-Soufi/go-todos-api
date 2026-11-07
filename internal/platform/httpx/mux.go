package httpx

import "net/http"

// JSONErrors makes the mux's built-in 404 and 405 responses use the standard
// JSON error body. Matched routes, including the mux's trailing-slash
// redirects, are served unchanged. A 405 keeps its Allow header.
func JSONErrors(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		cw := &statusOnly{header: w.Header()}
		mux.ServeHTTP(cw, r)
		switch cw.status {
		case http.StatusMethodNotAllowed:
			RespondError(w, cw.status, "METHOD_NOT_ALLOWED", "method not allowed")
		default:
			RespondError(w, http.StatusNotFound, "NOT_FOUND", "route not found")
		}
	})
}

// statusOnly records the status of the mux's fallback handler and drops its
// plain-text body; headers (such as Allow) go straight to the real writer.
type statusOnly struct {
	header http.Header
	status int
}

func (s *statusOnly) Header() http.Header         { return s.header }
func (s *statusOnly) Write(b []byte) (int, error) { return len(b), nil }
func (s *statusOnly) WriteHeader(status int)      { s.status = status }
