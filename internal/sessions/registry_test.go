package sessions

import "testing"

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
