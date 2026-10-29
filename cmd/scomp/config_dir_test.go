package main

import (
	"path/filepath"
	"testing"
)

func TestAgentConfigDirUsesAbsoluteHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, err := agentConfigDir()
	if err != nil || dir != filepath.Join(home, ".config", "scomp") {
		t.Fatalf("agentConfigDir() = %q, %v", dir, err)
	}
}

func TestAgentConfigDirRejectsMissingOrRelativeHome(t *testing.T) {
	for _, home := range []string{"", "relative/home"} {
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", "")
		if dir, err := agentConfigDir(); err == nil {
			t.Fatalf("HOME=%q: agentConfigDir() = %q, want an error", home, dir)
		}
	}
}

func TestAgentConfigDirFallsBackToAbsoluteXDGConfig(t *testing.T) {
	config := t.TempDir()
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", config)
	dir, err := agentConfigDir()
	if err != nil {
		// darwin and others derive the user config dir from HOME only.
		t.Skipf("no HOME-independent config dir on this platform: %v", err)
	}
	if dir != filepath.Join(config, "scomp") {
		t.Fatalf("agentConfigDir() = %q", dir)
	}
}
