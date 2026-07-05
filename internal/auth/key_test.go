package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func TestLoadOrCreateKey_PersistsAndReloads(t *testing.T) {
	dir := t.TempDir()

	priv1, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	priv2, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if !priv1.Equal(priv2) {
		t.Fatal("key not stable across reloads")
	}
}

func TestPublicKeyHex_VerifiesSignatures(t *testing.T) {
	priv, err := LoadOrCreateKey(t.TempDir())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	pubHex := PublicKeyHex(priv)
	pub, err := hex.DecodeString(pubHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("bad public key hex %q: %v", pubHex, err)
	}

	nonce := []byte("challenge-nonce-bytes")
	sig := ed25519.Sign(priv, nonce)
	if !ed25519.Verify(ed25519.PublicKey(pub), nonce, sig) {
		t.Fatal("signature did not verify against exported public key")
	}
	if ed25519.Verify(ed25519.PublicKey(pub), []byte("other"), sig) {
		t.Fatal("signature verified against the wrong message")
	}
}
