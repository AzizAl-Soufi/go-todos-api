// Package passkeytest is a software WebAuthn authenticator for tests. It
// answers the options the API hands out the way a browser and a platform
// authenticator would (ES256 keys, "none" attestation, user verification),
// so passkey registration and sign-in can be tested without a browser.
package passkeytest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
)

const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
	flagAttested     = 0x40
)

// Credential is a passkey held by the authenticator.
type Credential struct {
	ID         []byte
	UserHandle []byte
	key        *ecdsa.PrivateKey
	// SignCount is the counter sent with the next assertion, then
	// incremented. Zero keeps it at zero, like synced passkeys.
	SignCount uint32
}

// Authenticator answers for one web origin.
type Authenticator struct {
	Origin string
}

func New(origin string) *Authenticator { return &Authenticator{Origin: origin} }

var b64 = base64.RawURLEncoding

// options is the part of {"publicKey": {...}} the authenticator reads.
type options struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RPID      string `json:"rpId"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"publicKey"`
}

func parse(t testing.TB, raw []byte) options {
	t.Helper()
	var o options
	if err := json.Unmarshal(raw, &o); err != nil || o.PublicKey.Challenge == "" {
		t.Fatalf("passkeytest: bad options %s: %v", raw, err)
	}
	return o
}

func (a *Authenticator) clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": a.Origin, "crossOrigin": false})
	return b
}

func authData(rpID string, flags byte, count uint32, attested []byte) []byte {
	h := sha256.Sum256([]byte(rpID))
	out := append(h[:], flags)
	out = binary.BigEndian.AppendUint32(out, count)
	return append(out, attested...)
}

// Create answers creation options (from navigator.credentials.create) and
// returns the PublicKeyCredential as JSON, plus the new credential.
func (a *Authenticator) Create(t testing.TB, rawOptions []byte) (json.RawMessage, *Credential) {
	t.Helper()
	o := parse(t, rawOptions)
	userHandle, err := b64.DecodeString(o.PublicKey.User.ID)
	if err != nil {
		t.Fatalf("passkeytest: user id: %v", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := &Credential{ID: make([]byte, 16), UserHandle: userHandle, key: key}
	_, _ = rand.Read(c.ID)

	ecdh, err := key.PublicKey.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	point := ecdh.Bytes() // 0x04 || X || Y
	cose, err := webauthncbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: point[1:33], -3: point[33:]})
	if err != nil {
		t.Fatal(err)
	}
	attested := make([]byte, 16) // AAGUID: all zero
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(c.ID)))
	attested = append(append(attested, c.ID...), cose...)

	att, err := webauthncbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData(o.PublicKey.RP.ID, flagUserPresent|flagUserVerified|flagAttested, 0, attested),
	})
	if err != nil {
		t.Fatal(err)
	}
	return marshal(t, map[string]any{
		"id": b64.EncodeToString(c.ID), "rawId": b64.EncodeToString(c.ID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(a.clientData("webauthn.create", o.PublicKey.Challenge)),
			"attestationObject": b64.EncodeToString(att),
		},
		"clientExtensionResults": map[string]any{"credProps": map[string]any{"rk": true}},
	}), c
}

// Get answers request options (from navigator.credentials.get) with c, as
// a discoverable credential would, and returns the PublicKeyCredential JSON.
func (a *Authenticator) Get(t testing.TB, rawOptions []byte, c *Credential) json.RawMessage {
	t.Helper()
	o := parse(t, rawOptions)
	data := authData(o.PublicKey.RPID, flagUserPresent|flagUserVerified, c.SignCount, nil)
	if c.SignCount > 0 {
		c.SignCount++
	}
	clientData := a.clientData("webauthn.get", o.PublicKey.Challenge)
	digest := sha256.Sum256(clientData)
	signed := sha256.Sum256(append(data, digest[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, c.key, signed[:])
	if err != nil {
		t.Fatal(err)
	}
	return marshal(t, map[string]any{
		"id": b64.EncodeToString(c.ID), "rawId": b64.EncodeToString(c.ID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(clientData),
			"authenticatorData": b64.EncodeToString(data),
			"signature":         b64.EncodeToString(sig),
			"userHandle":        b64.EncodeToString(c.UserHandle),
		},
		"clientExtensionResults": map[string]any{},
	})
}

func marshal(t testing.TB, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
