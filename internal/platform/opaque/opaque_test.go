package opaque

import "testing"

func TestNew(t *testing.T) {
	a, hashA, err := New()
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := New()
	if a == b || len(a) != 43 || hashA != Hash(a) || len(hashA) != 64 || hashA == a {
		t.Fatalf("token %q hash %q", a, hashA)
	}
}
