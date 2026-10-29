package main

import (
	"fmt"
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
				&serverMsgEnv{reg: reg, modes: modes, lazyMap: map[string]*lazySession{}},
			)

			if saw := waitForOutput("45 123", time.Second); saw != tt.resized {
				t.Fatalf("resized = %v, want %v (output %q)", saw, tt.resized, reg.Snapshot(info.ID))
			}
		})
	}
}

func TestClientAttachSizeOnlyActivatesLazySessions(t *testing.T) {
	tests := []struct {
		name             string
		cols, rows       uint16
		wantCols, wantRo uint16
	}{
		{"read-only attach uses default", 0, 0, 80, 24},
		{"writer attach sizes new PTY", 132, 40, 132, 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotCols, gotRows uint16
			lazy := map[string]*lazySession{"lazy": {
				id: "lazy", mode: sessions.ModeReadonly,
				activate: func(cols, rows uint16) error {
					gotCols, gotRows = cols, rows
					return fmt.Errorf("stop after recording the size")
				},
			}}
			handleServerMsg(
				protocol.ServerMsg{Type: "client_attach", SessionID: "lazy", ClientID: "c1", Cols: tt.cols, Rows: tt.rows},
				&serverMsgEnv{
					reg: sessions.New(), modes: map[string]sessions.SessionMode{}, lazyMap: lazy,
					send: func(protocol.AgentMsg) {}, requestSnapshot: func(string, string) {},
				},
			)
			if gotCols != tt.wantCols || gotRows != tt.wantRo {
				t.Fatalf("activate(%d, %d), want (%d, %d)", gotCols, gotRows, tt.wantCols, tt.wantRo)
			}
		})
	}
}

// TestClientAttachNeverResizesActivePTY attaches to a running full-mode PTY
// with a size and checks the PTY keeps its original dimensions.
func TestClientAttachNeverResizesActivePTY(t *testing.T) {
	reg := sessions.New()
	info, _, err := reg.Create("sh", []string{"-c", "while :; do stty size; sleep 0.05; done"}, 80, 24)
	if err != nil {
		t.Skipf("cannot start PTY: %v", err)
	}
	t.Cleanup(func() { reg.Remove(info.ID) })
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(string(reg.Snapshot(info.ID)), "24 80") {
		if time.Now().After(deadline) {
			t.Fatalf("PTY never reported its initial size: %q", reg.Snapshot(info.ID))
		}
		time.Sleep(20 * time.Millisecond)
	}
	modes := map[string]sessions.SessionMode{info.ID: sessions.ModeFull}
	handleServerMsg(
		protocol.ServerMsg{Type: "client_attach", SessionID: info.ID, ClientID: "c1", Cols: 123, Rows: 45},
		&serverMsgEnv{
			reg: reg, modes: modes, lazyMap: map[string]*lazySession{},
			send: func(protocol.AgentMsg) {}, requestSnapshot: func(string, string) {},
		},
	)
	time.Sleep(300 * time.Millisecond)
	if out := string(reg.Snapshot(info.ID)); strings.Contains(out, "45 123") {
		t.Fatalf("attach changed the PTY size: %q", out)
	}
}
