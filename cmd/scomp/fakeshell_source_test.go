package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/smart-bit-me/scomp-agent/internal/sessions"
	"github.com/smart-bit-me/scomp-agent/internal/stream"
	"github.com/smart-bit-me/scomp-agent/protocol"
)

func TestFakeSourceAdvertisesStableFullSessions(t *testing.T) {
	a, _, _ := newFakeSource(sessions.New(), "uid-a", "review-host").snapshotAndSubscribe()
	b, _, _ := newFakeSource(sessions.New(), "uid-a", "review-host").snapshotAndSubscribe()
	other, _, _ := newFakeSource(sessions.New(), "uid-b", "review-host").snapshotAndSubscribe()
	var names []string
	for i, ls := range a {
		names = append(names, ls.cmd)
		if ls.mode != sessions.ModeFull {
			t.Errorf("%s mode = %q, want full", ls.cmd, ls.mode)
		}
		if strings.HasPrefix(ls.cmd, "tmux:") || strings.HasPrefix(ls.cmd, "screen:") {
			t.Errorf("%s would get multiplexer keys in the apps", ls.cmd)
		}
		if ls.id != b[i].id || len(ls.id) != 32 {
			t.Errorf("%s id not stable across restarts: %q vs %q", ls.cmd, ls.id, b[i].id)
		}
		if ls.id == other[i].id {
			t.Errorf("%s id shared between agent identities", ls.cmd)
		}
	}
	if got := strings.Join(names, ","); got != "shell,agent,logs" {
		t.Errorf("sessions = %s", got)
	}
}

// TestFakeSessionThroughServerMessages drives a fake session the way the
// relay does: client_attach activates it, input reaches the shell, and its
// output arrives on the registry terminal.
func TestFakeSessionThroughServerMessages(t *testing.T) {
	reg := sessions.New()
	lazy, _, _ := newFakeSource(reg, "uid-test", "review-host").snapshotAndSubscribe()
	shell := lazy[0]
	lazyMap := map[string]*lazySession{shell.id: &shell}
	modes := map[string]sessions.SessionMode{}
	var started []string
	env := &serverMsgEnv{
		reg: reg, modes: modes, lazyMap: lazyMap,
		send:            func(protocol.AgentMsg) {},
		sendSessionAdd:  func(string, string, time.Time, sessions.SessionMode, bool, uint16, uint16) {},
		startForwarder:  func(id string) { started = append(started, id) },
		requestSnapshot: func(string, string) {},
	}
	handleServerMsg(protocol.ServerMsg{Type: "client_attach", SessionID: shell.id, ClientID: "c1", Cols: 60, Rows: 20}, env)
	term, ok := reg.Get(shell.id)
	if !ok || len(started) != 1 || modes[shell.id] != sessions.ModeFull {
		t.Fatalf("not activated: ok=%v started=%v mode=%q", ok, started, modes[shell.id])
	}
	defer reg.Remove(shell.id)

	events := make(chan stream.Event, 256)
	term.Subscribe(events, nil)
	defer term.Unsubscribe(events)
	input := base64.StdEncoding.EncodeToString([]byte("echo fake-$HOSTNAME\r"))
	handleServerMsg(protocol.ServerMsg{Type: "input", SessionID: shell.id, Data: input}, env)
	var out strings.Builder
	deadline := time.After(3 * time.Second)
	for !strings.Contains(out.String(), "fake-review-host\r\n") {
		select {
		case ev := <-events:
			out.Write(ev.Data)
		case <-deadline:
			t.Fatalf("no shell output; got %q", out.String())
		}
	}
	// A second attach must reuse the running session, not restart it.
	handleServerMsg(protocol.ServerMsg{Type: "client_attach", SessionID: shell.id, ClientID: "c2"}, env)
	if again, _ := reg.Get(shell.id); again != term {
		t.Fatal("second attach replaced the running fake session")
	}
}

func TestAgentHostname(t *testing.T) {
	if got := agentHostname("custom", true); got != "custom" {
		t.Errorf("flag ignored: %q", got)
	}
	if got := agentHostname("", true); got != "review-host" {
		t.Errorf("fake default = %q", got)
	}
	if got := agentHostname("", false); got == "review-host" || got == "" {
		t.Errorf("regular default = %q, want the OS hostname", got)
	}
}
