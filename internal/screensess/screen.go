// Package screensess lists active GNU screen sessions so the web client can
// attach to them via "screen -x <session>", which is the multi-display mode
// that mirrors output to all clients without detaching the existing one.
package screensess

import (
	"bytes"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// SocketDir returns the directory where screen writes per-session socket files.
// A CREATE event in this directory means a new screen session started.
func SocketDir() string {
	// prefer $SCREENDIR if set
	if dir := os.Getenv("SCREENDIR"); dir != "" {
		return dir
	}
	u, err := user.Current()
	if err != nil {
		return ""
	}
	// modern Linux (systemd-based distros)
	for _, candidate := range []string{
		"/run/screen/S-" + u.Username,
		"/var/run/screen/S-" + u.Username,
		"/tmp/screens/S-" + u.Username,
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "/run/screen/S-" + u.Username
}

type Session struct {
	FullName string `json:"fullName"` // "pid.name" as screen uses it
	Name     string `json:"name"`     // display name (part after first dot)
	Status   string `json:"status"`   // "Attached" | "Detached"
}

// List runs "screen -ls" and returns the active sessions.
// Returns an empty slice (not an error) when screen is not installed or has no sessions.
func List() []Session {
	var buf bytes.Buffer
	cmd := exec.Command("screen", "-ls")
	cmd.Stdout = &buf
	cmd.Run() // screen -ls exits non-zero even when sessions exist — ignore error
	return parseScreenOutput(buf.String())
}

// parseScreenOutput parses "screen -ls" output into Sessions.
// Exported for testing.
func parseScreenOutput(out string) []Session {
	var result []Session
	seenDisplayNames := make(map[string]struct{})
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !strings.Contains(trimmed, ".") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		fullName := fields[0]
		status := strings.Trim(fields[1], "()")
		if status != "Attached" && status != "Detached" {
			continue
		}

		name := fullName
		if i := strings.Index(fullName, "."); i >= 0 {
			name = fullName[i+1:]
		}
		// Screen can expose more than one PID-prefixed socket for the same display
		// name. The display name is what the catalog and clients treat as identity;
		// retain the first socket so its relay session ID remains stable.
		if _, exists := seenDisplayNames[name]; exists {
			continue
		}
		seenDisplayNames[name] = struct{}{}

		result = append(result, Session{
			FullName: fullName,
			Name:     name,
			Status:   status,
		})
	}
	return result
}

// AttachArgs returns the command + args to multi-attach to a screen session.
// -x enables multi-display mode without resizing the existing session.
func AttachArgs(fullName string) (string, []string) {
	return "screen", []string{"-x", fullName}
}
