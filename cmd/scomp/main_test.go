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

func TestAgentHelloReportsBuildVersion(t *testing.T) {
	previousVersion := version
	version = "v0.2.4"
	t.Cleanup(func() { version = previousVersion })

	hello := newAgentHello("agent-id", "host-a", "public-key")
	if hello.Type != "hello" || hello.UID != "agent-id" ||
		hello.Hostname != "host-a" || hello.PubKey != "public-key" ||
		hello.Version != "v0.2.4" {
		t.Fatalf("unexpected hello: %#v", hello)
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

func TestInputAllowed(t *testing.T) {
	tests := []struct {
		name string
		mode sessions.SessionMode
		data string
		want bool
	}{
		{"full arbitrary", sessions.ModeFull, "rm -rf ~\r", true},
		{"full empty", sessions.ModeFull, "", true},
		{"approved accepted", sessions.ModeApproved, "y\r", true},
		{"approved rejected", sessions.ModeApproved, "ls\r", false},
		{"readonly approved seq", sessions.ModeReadonly, "y\r", false},
		{"readonly arbitrary", sessions.ModeReadonly, "ls\r", false},
		{"missing mode", "", "\r", false},
		{"missing mode arbitrary", "", "ls\r", false},
		{"unknown mode", sessions.SessionMode("FULL"), "ls\r", false},
		{"unknown mode approved seq", sessions.SessionMode("approved"), "y\r", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inputAllowed(tt.mode, []byte(tt.data)); got != tt.want {
				t.Fatalf("inputAllowed(%q, %q) = %v, want %v", tt.mode, tt.data, got, tt.want)
			}
		})
	}
}

func TestIsApproved(t *testing.T) {
	accepted := []string{"y\r", "y\n", "n\r", "n\n", "\x03", "\r", "\t", "\x1b"}
	rejected := []string{
		"", "\n", "\r\n", "y", "n", "Y\r", "N\n", "\t\t", "\x03\x03", "yes\r",
		"\x1b[A", "\x1b[B", "\x1b[C", "\x1b[D", "\x1bOA", "\x1b\r", "y\r\n",
	}
	for _, s := range accepted {
		if !isApproved([]byte(s)) {
			t.Errorf("isApproved(%q) = false, want true", s)
		}
	}
	for _, s := range rejected {
		if isApproved([]byte(s)) {
			t.Errorf("isApproved(%q) = true, want false", s)
		}
	}
}
