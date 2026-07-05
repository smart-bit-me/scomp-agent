package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadOrCreateKey reads the agent's Ed25519 private key from configDir/key.
// The file holds the 32-byte hex seed. If it is missing or malformed a new
// keypair is generated and persisted with mode 0600.
//
// The key is the agent's identity secret: the server pins uid → public key on
// first connect and rejects any later connection presenting a different key, so
// knowledge of the uid alone is no longer enough to impersonate the agent (F3).
func LoadOrCreateKey(configDir string) (ed25519.PrivateKey, error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(configDir, "key")
	if data, err := os.ReadFile(path); err == nil {
		seed, err := hex.DecodeString(strings.TrimSpace(string(data)))
		if err == nil && len(seed) == ed25519.SeedSize {
			return ed25519.NewKeyFromSeed(seed), nil
		}
	}
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}
	seed := hex.EncodeToString(priv.Seed())
	if err := os.WriteFile(path, []byte(seed+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("saving key: %w", err)
	}
	return priv, nil
}

// PublicKeyHex returns the hex-encoded Ed25519 public key for a private key.
func PublicKeyHex(priv ed25519.PrivateKey) string {
	return hex.EncodeToString(priv.Public().(ed25519.PublicKey))
}
