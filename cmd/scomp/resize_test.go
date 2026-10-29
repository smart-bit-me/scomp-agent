package main

import (
	"strings"
	"testing"
	"time"

	"github.com/smart-bit-me/scomp-agent/internal/sessions"
	"github.com/smart-bit-me/scomp-agent/protocol"
)

func TestResizeAllowed(t *testing.T) {
	tests := []struct {
		mode sessions.SessionMode
		want bool
	}{
		{sessions.ModeFull, true},
		{sessions.ModeApproved, true},
		{sessions.ModeReadonly, false},
		{"", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		if got := resizeAllowed(tt.mode); got != tt.want {
			t.Errorf("resizeAllowed(%q) = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

// TestHandleResizeHonorsMode drives handleServerMsg against a real PTY that
// reports its size, checking that only full/approved-only sessions resize.
func TestHandleResizeHonorsMode(t *testing.T) {
	tests := []struct {
		name    string
		mode    sessions.SessionMode
		present bool
		resized bool
	}{
		{"full", sessions.ModeFull, true, true},
		{"approved-only", sessions.ModeApproved, true, true},
		{"readonly", sessions.ModeReadonly, true, false},
		{"missing mode", "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := sessions.New()
			info, _, err := reg.Create("sh", []string{"-c", "while :; do stty size; sleep 0.05; done"}, 80, 24)
			if err != nil {
				t.Skipf("cannot start PTY: %v", err)
			}
			t.Cleanup(func() { reg.Remove(info.ID) })
			waitForOutput := func(want string, timeout time.Duration) bool {
				deadline := time.Now().Add(timeout)
				for time.Now().Before(deadline) {
					if strings.Contains(string(reg.Snapshot(info.ID)), want) {
						return true
					}
					time.Sleep(20 * time.Millisecond)
				}
				return false
			}
			if !waitForOutput("24 80", 5*time.Second) {
				t.Fatalf("PTY never reported its initial size: %q", reg.Snapshot(info.ID))
			}

			modes := map[string]sessions.SessionMode{}
			if tt.present {
				modes[info.ID] = tt.mode
			}
			handleServerMsg(
				protocol.ServerMsg{Type: "resize", SessionID: info.ID, Cols: 123, Rows: 45},
				reg, modes, map[string]*lazySession{},
				func(protocol.AgentMsg) {},
				func(string, string, time.Time, sessions.SessionMode, bool, uint16, uint16) {},
				func(string) {},
				func(string, string) {},
			)

			if saw := waitForOutput("45 123", time.Second); saw != tt.resized {
				t.Fatalf("resized = %v, want %v (output %q)", saw, tt.resized, reg.Snapshot(info.ID))
			}
		})
	}
}
