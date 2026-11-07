package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/todos/domain"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

// body is a JSON object decoded field by field, so partial updates can tell
// an absent field (leave unchanged) from an explicit null (clear it).
type body map[string]json.RawMessage

// decodeBody reads a JSON object. With requireFields, an empty object is
// rejected (a partial update must change something).
func decodeBody(r *http.Request, requireFields bool) (body, error) {
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, apperrors.ErrInvalidRequestBody
	}
	var b body
	if err := json.Unmarshal(raw, &b); err != nil || b == nil || requireFields && len(b) == 0 {
		return nil, apperrors.ErrInvalidRequestBody
	}
	return b, nil
}

func invalidField(key, want string) error {
	return apperrors.Validation("INVALID_FIELD", fmt.Sprintf("%s must be %s", key, want))
}

func isNull(v json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(v), []byte("null")) }

// field decodes key when present. It reports whether the key was
// present and non-null; null returns onNull (nil means null is allowed).
func field[T any](b body, key, want string, onNull error) (*T, bool, error) {
	raw, ok := b[key]
	if !ok {
		return nil, false, nil
	}
	if isNull(raw) {
		if onNull != nil {
			return nil, false, onNull
		}
		return nil, true, nil
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false, invalidField(key, want)
	}
	return &v, true, nil
}

// str is an optional string field that can't be null.
func (b body) str(key string, onNull error) (*string, error) {
	v, _, err := field[string](b, key, "a string", firstErr(onNull, invalidField(key, "a string")))
	return v, err
}

func (b body) boolean(key string, onNull error) (*bool, error) {
	v, _, err := field[bool](b, key, "true or false", firstErr(onNull, invalidField(key, "true or false")))
	return v, err
}

func (b body) strings(key string) (*[]string, error) {
	v, _, err := field[[]string](b, key, "an array of strings", invalidField(key, "an array of strings"))
	return v, err
}

// id is an optional reference that null (or "") clears.
func (b body) id(key string) (domain.Patch[string], error) {
	v, present, err := field[string](b, key, "a string or null", nil)
	if err != nil || !present {
		return domain.Patch[string]{Set: present}, err
	}
	if v == nil || *v == "" {
		return domain.Clear[string](), nil
	}
	return domain.Set(*v), nil
}

// time is an optional timestamp (RFC 3339, or a YYYY-MM-DD date meaning
// midnight UTC) that null clears.
func (b body) time(key string) (domain.Patch[time.Time], error) {
	v, present, err := field[string](b, key, "an RFC 3339 timestamp, a YYYY-MM-DD date or null", nil)
	if err != nil || !present {
		return domain.Patch[time.Time]{Set: present}, err
	}
	if v == nil {
		return domain.Clear[time.Time](), nil
	}
	t, err := parseTime(*v)
	if err != nil {
		return domain.Patch[time.Time]{}, invalidField(key, "an RFC 3339 timestamp, a YYYY-MM-DD date or null")
	}
	return domain.Set(t), nil
}

func (b body) status(key string) (*domain.Status, error) {
	v, err := b.str(key, domain.ErrInvalidStatus)
	if err != nil || v == nil {
		return nil, err
	}
	s, err := domain.ParseStatus(*v)
	return &s, err
}

func (b body) priority(key string) (*domain.Priority, error) {
	v, err := b.str(key, domain.ErrInvalidPriority)
	if err != nil || v == nil {
		return nil, err
	}
	p, err := domain.ParsePriority(*v)
	return &p, err
}

// parseTime accepts RFC 3339 timestamps and plain dates.
func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse(time.DateOnly, s)
}

// firstErr returns the first non-nil error.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
