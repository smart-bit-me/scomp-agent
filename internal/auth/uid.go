package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadOrCreateUID reads the UID from configDir/uid.
// If the file does not exist or is malformed a new 32-hex-char UID is generated
// and persisted there with mode 0600.
func LoadOrCreateUID(configDir string) (string, error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(configDir, "uid")
	if data, err := os.ReadFile(path); err == nil {
		uid := strings.TrimSpace(string(data))
		if len(uid) == 32 {
			return uid, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating uid: %w", err)
	}
	uid := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(uid+"\n"), 0600); err != nil {
		return "", fmt.Errorf("saving uid: %w", err)
	}
	return uid, nil
}
