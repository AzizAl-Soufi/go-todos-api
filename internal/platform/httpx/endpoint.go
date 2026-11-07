package httpx

import "net/http"

// Endpoint handles a request and returns the response status and body, or an
// error. It keeps handlers free of response plumbing: errors go through
// RespondErr, 204 is written without a body, anything else as JSON.
type Endpoint func(r *http.Request) (status int, body any, err error)

func (e Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	status, body, err := e(r)
	switch {
	case err != nil:
		RespondErr(w, r, err)
	case status == http.StatusNoContent:
		RespondNoContent(w)
	default:
		RespondJSON(w, status, body)
	}
}
