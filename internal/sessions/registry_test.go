package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smart-bit-me/scomp-agent/internal/pty"
)

func TestNewID_Unique(t *testing.T) {
	ids := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := newID()
		if ids[id] {
			t.Fatalf("duplicate ID: %q", id)
		}
		ids[id] = true
	}
}

func TestSourceKey_Tmux_AttachSession(t *testing.T) {
	key := sourceKey("tmux", []string{"attach-session", "-t", "main"})
	if key != "tmux:main" {
		t.Fatalf("got %q, want tmux:main", key)
	}
}

func TestSourceKey_Tmux_NewSession(t *testing.T) {
	key := sourceKey("tmux", []string{"new-session", "-A", "-t", "main", "-s", "scomp-main"})
	if key != "tmux:scomp-main" {
		t.Fatalf("got %q, want tmux:scomp-main", key)
	}
}

func TestSourceKey_Screen(t *testing.T) {
	key := sourceKey("screen", []string{"-x", "68764.work"})
	if key != "screen:68764.work" {
		t.Fatalf("got %q, want screen:68764.work", key)
	}
}

func TestSourceKey_Screen_NoAFlag(t *testing.T) {
	// Ensure the old -A -x form is no longer recognized.
	key := sourceKey("screen", []string{"-A", "-x", "68764.work"})
	if key != "" {
		t.Fatalf("expected empty key for old -A -x form, got %q", key)
	}
}

func TestSourceKey_Unknown(t *testing.T) {
	key := sourceKey("/bin/bash", nil)
	if key != "" {
		t.Fatalf("expected empty key, got %q", key)
	}
}

func TestSourceKey_FullPath(t *testing.T) {
	key := sourceKey("/usr/bin/tmux", []string{"attach-session", "-t", "dev"})
	if key != "tmux:dev" {
		t.Fatalf("got %q, want tmux:dev", key)
	}
}

func TestSourceKeyTmuxExactTarget(t *testing.T) {
	key := sourceKey("tmux", []string{"attach-session", "-t", "=main"})
	if key != "tmux:main" {
		t.Fatalf("got %q, want tmux:main", key)
	}
}

// endedSession returns a PTY session whose process has already exited.
func endedSession(t *testing.T) *pty.Session {
	t.Helper()
	sess, err := pty.New("sh", []string{"-c", "exit 0"}, 80, 24)
	if err != nil {
		t.Skipf("cannot start PTY: %v", err)
	}
	select {
	case <-sess.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("short-lived PTY did not end")
	}
	return sess
}

// TestRegistryHidesEndedSessionsBeforeCleanup covers the window between a
// PTY ending and its cleanup goroutine removing it: lookups and source
// dedup must not hand out the dead session.
func TestRegistryHidesEndedSessionsBeforeCleanup(t *testing.T) {
	r := New()
	dead := endedSession(t)
	r.entries["old"] = &entry{Info: Info{ID: "old"}, session: dead, source: "tmux:scomp-work"}
	r.sourceToID["tmux:scomp-work"] = "old"

	if _, ok := r.Get("old"); ok {
		t.Fatal("Get returned an ended session")
	}
	// A stand-in "tmux" so the attach command has the same source key.
	tmux := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(tmux, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	args := []string{"new-session", "-A", "-t", "work", "-s", "scomp-work"}
	if sourceKey(tmux, args) != "tmux:scomp-work" {
		t.Fatal("test command does not share the dead session's source key")
	}
	_, sess, err := r.CreateWithID("new", tmux, args, 80, 24)
	if err != nil {
		t.Skipf("cannot start PTY: %v", err)
	}
	defer r.Remove("new")
	if sess == dead {
		t.Fatal("CreateWithID aliased an ended session for the same source")
	}
}

// TestRegistryLateCleanupKeepsReactivatedSession: a lazy session reactivated
// under the same id must survive the old session's cleanup.
func TestRegistryLateCleanupKeepsReactivatedSession(t *testing.T) {
	r := New()
	dead := endedSession(t)
	_, live, err := r.CreateWithID("id", "sh", []string{"-c", "sleep 30"}, 80, 24)
	if err != nil {
		t.Skipf("cannot start PTY: %v", err)
	}
	defer r.Remove("id")
	r.forgetWhenDone("id", dead) // the old session's cleanup, running late
	if got, ok := r.Get("id"); !ok || got != live {
		t.Fatal("late cleanup removed the reactivated session")
	}
}
