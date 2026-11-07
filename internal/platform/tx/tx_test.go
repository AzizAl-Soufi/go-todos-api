package tx

import (
	"context"
	"errors"
	"testing"
)

func TestNone(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "v")
	boom := errors.New("boom")

	err := None{}.WithinTx(ctx, func(inner context.Context) error {
		if inner.Value(key{}) != "v" {
			t.Fatal("fn must receive the caller's context")
		}
		return boom
	})
	if err != boom {
		t.Fatalf("err = %v, want fn's error", err)
	}
}
