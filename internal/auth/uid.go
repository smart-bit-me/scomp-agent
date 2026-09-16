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
// If the file does not exist, a new 32-hex-char UID is created atomically. A
// malformed existing identity fails closed instead of silently rotating a UID
// that may already be paired at the relay.
func LoadOrCreateUID(configDir string) (string, error) {
	if err := preparePrivateDir(configDir); err != nil {
		return "", err
	}
	path := filepath.Join(configDir, "uid")
	if data, exists, err := readPrivateFile(path); err != nil {
		return "", fmt.Errorf("reading uid: %w", err)
	} else if exists {
		uid := strings.TrimSpace(string(data))
		decoded, decodeErr := hex.DecodeString(uid)
		if decodeErr == nil && len(decoded) == 16 {
			return uid, nil
		}
		return "", fmt.Errorf("invalid uid file %s; restore it or remove it and re-pair explicitly", path)
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating uid: %w", err)
	}
	uid := hex.EncodeToString(b)
	if err := createPrivateFile(path, []byte(uid+"\n")); os.IsExist(err) {
		return LoadOrCreateUID(configDir)
	} else if err != nil {
		return "", fmt.Errorf("saving uid: %w", err)
	}
	return uid, nil
}
