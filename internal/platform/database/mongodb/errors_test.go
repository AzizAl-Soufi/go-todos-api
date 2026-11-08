package mongodb

import (
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"

	apperrors "github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

func TestMapNotFound(t *testing.T) {
	other := errors.New("network")

	if err := MapNotFound(mongo.ErrNoDocuments); !errors.Is(err, apperrors.ErrNotFound) {
		t.Errorf("ErrNoDocuments -> %v, want ErrNotFound", err)
	}
	if err := MapNotFound(other); err != other {
		t.Errorf("other error -> %v, want it unchanged", err)
	}
	if err := MapNotFound(nil); err != nil {
		t.Errorf("nil -> %v, want nil", err)
	}
}

func TestRequireDeleted(t *testing.T) {
	other := errors.New("network")

	tests := []struct {
		name   string
		result *mongo.DeleteResult
		err    error
		want   error
	}{
		{"deleted", &mongo.DeleteResult{DeletedCount: 1}, nil, nil},
		{"nothing matched", &mongo.DeleteResult{DeletedCount: 0}, nil, apperrors.ErrNotFound},
		{"driver error", nil, other, other},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequireDeleted(tt.result, tt.err); !errors.Is(got, tt.want) || (tt.want == nil && got != nil) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
