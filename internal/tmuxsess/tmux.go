package tmuxsess

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SocketDir returns the directory where the tmux server socket lives.
// A CREATE event in this directory means a new tmux server started.
func SocketDir() string {
	uid := os.Getuid()
	if dir := os.Getenv("TMUX_TMPDIR"); dir != "" {
		return fmt.Sprintf("%s/tmux-%d", dir, uid)
	}
	if dir := os.Getenv("TMPDIR"); dir != "" {
		return fmt.Sprintf("%s/tmux-%d", dir, uid)
	}
	return fmt.Sprintf("/tmp/tmux-%d", uid)
}

const sccompPrefix = "scomp-"

type Session struct {
	Name    string `json:"name"`
	Windows int    `json:"windows"`
}

// List runs "tmux list-sessions" and returns the active sessions.
// Returns an empty slice (not an error) when tmux is not installed or has no sessions.
func List() []Session {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}|#{session_windows}").Output()
	if err != nil {
		return []Session{}
	}
	return parseOutput(string(out))
}

// parseOutput parses "tmux list-sessions" output into Sessions.
// Exported for testing.
func parseOutput(out string) []Session {
	var result []Session
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		name := parts[0]
		if strings.HasPrefix(name, sccompPrefix) {
			continue
		}
		windows := 1
		if len(parts) == 2 {
			n := 0
			for _, c := range parts[1] {
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
				}
			}
			if n > 0 {
				windows = n
			}
		}
		result = append(result, Session{Name: name, Windows: windows})
	}
	return result
}

// ExactTarget returns a -t target-session that matches only the session named
// exactly name. Without the "=" prefix tmux falls back to prefix and fnmatch
// matching, so a stale "X" could resolve to "Xprod".
func ExactTarget(name string) string {
	return "=" + name
}

// LinkedSessionName is the name of the agent-owned session grouped with name.
func LinkedSessionName(name string) string {
	return sccompPrefix + name
}

// AttachArgs returns args to attach to an existing tmux session.
func AttachArgs(name string) (string, []string) {
	return "tmux", []string{"attach-session", "-t", ExactTarget(name)}
}

// LinkedSessionArgs creates (or reattaches to) a session in the same window
// group as name but with independent dimensions. -s is always an exact name.
func LinkedSessionArgs(name string) (string, []string) {
	return "tmux", []string{"new-session", "-A", "-t", ExactTarget(name), "-s", LinkedSessionName(name)}
}

// KillLinkedSession kills the agent-owned session linked to name, and never a
// different session whose name merely starts with it.
func KillLinkedSession(name string) error {
	return exec.Command("tmux", "kill-session", "-t", ExactTarget(LinkedSessionName(name))).Run()
}
