package main

import (
	"testing"

	"github.com/smart-bit-me/scomp-agent/internal/sessions"
)

func TestParseSessionMode(t *testing.T) {
	tests := []struct {
		input string
		want  sessions.SessionMode
	}{
		{input: "full", want: sessions.ModeFull},
		{input: "readonly", want: sessions.ModeReadonly},
		{input: "approved-only", want: sessions.ModeApproved},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSessionMode(tt.input)
			if err != nil {
				t.Fatalf("parseSessionMode(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("parseSessionMode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSessionModeRejectsUnknownValue(t *testing.T) {
	if got, err := parseSessionMode("ful"); err == nil {
		t.Fatalf("parseSessionMode() = %q, want error", got)
	}
}

func TestRestoreLazySessionAfterScreenDisplayExit(t *testing.T) {
	entry := lazySession{id: "stable-id", cmd: "screen:yughljkkok"}
	available := map[string]lazySession{entry.id: entry}
	lazy := make(map[string]*lazySession)

	got, ok := restoreLazySession(entry.id, available, lazy)
	if !ok || got.id != entry.id {
		t.Fatalf("restoreLazySession() = %#v, %v", got, ok)
	}
	if lazy[entry.id] == nil || lazy[entry.id].cmd != entry.cmd {
		t.Fatalf("lazy session was not restored: %#v", lazy)
	}
}

func TestRestoreLazySessionDoesNotReviveRemovedCatalogEntry(t *testing.T) {
	lazy := make(map[string]*lazySession)
	if _, ok := restoreLazySession("removed", map[string]lazySession{}, lazy); ok {
		t.Fatal("removed catalog entry was restored")
	}
	if len(lazy) != 0 {
		t.Fatalf("unexpected lazy sessions: %#v", lazy)
	}
}
