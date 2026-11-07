// Package testutil holds helpers shared by tests across packages.
// It must only be imported from _test.go files.
package testutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// NewRSAKey generates a fresh 2048-bit RSA key.
func NewRSAKey(t testing.TB) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return key
}

// WriteRSAKeyFiles writes key as private.pem (PKCS#1) and public.pem (PKIX)
// into a fresh temp dir and returns both paths.
func WriteRSAKeyFiles(t testing.TB, key *rsa.PrivateKey) (privPath, pubPath string) {
	t.Helper()

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	dir := t.TempDir()
	privPath = filepath.Join(dir, "private.pem")
	pubPath = filepath.Join(dir, "public.pem")
	writePEM(t, privPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key))
	writePEM(t, pubPath, "PUBLIC KEY", pubDER)
	return privPath, pubPath
}

func writePEM(t testing.TB, path, blockType string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ExpectErrorIs runs each named call as a subtest and checks its error matches target.
func ExpectErrorIs(t *testing.T, target error, cases map[string]func() error) {
	t.Helper()
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, target) {
				t.Fatalf("err = %v, want %v", err, target)
			}
		})
	}
}
