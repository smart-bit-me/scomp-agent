package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
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

func TestLoadOrCreateKeyRejectsMalformedExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := os.WriteFile(path, []byte("corrupt\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(dir); err == nil {
		t.Fatal("malformed key was silently replaced")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "corrupt\n" {
		t.Fatalf("malformed key was modified: %q", data)
	}
}

func TestLoadOrCreateKeyRepairsPermissions(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadOrCreateKey(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "key")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("mode = %o, want 600", got)
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
