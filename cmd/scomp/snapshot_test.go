package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/smart-bit-me/scomp-agent/internal/sessions"
	"github.com/smart-bit-me/scomp-agent/protocol"
)

func TestSendSnapshotChunksBoundarySizes(t *testing.T) {
	chunk := protocol.SnapshotChunkBytes
	for _, size := range []int{0, 1, chunk - 1, chunk, chunk + 1, 786 << 10, 1 << 20} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i * 7)
		}
		var msgs []protocol.AgentMsg
		sendSnapshot("s1", "c1", data, func(msg protocol.AgentMsg) bool {
			msgs = append(msgs, msg)
			return true
		})

		wantChunks := (size + chunk - 1) / chunk
		if len(msgs) != wantChunks+2 {
			t.Fatalf("size %d: %d messages, want %d", size, len(msgs), wantChunks+2)
		}
		start, end := msgs[0], msgs[len(msgs)-1]
		if start.Type != "snapshot_start" || start.Total != size || start.ClientID != "c1" || start.SessionID != "s1" {
			t.Fatalf("size %d: start = %+v", size, start)
		}
		if end.Type != "snapshot_end" || end.Seq != uint32(wantChunks) || end.ClientID != "c1" {
			t.Fatalf("size %d: end = %+v", size, end)
		}
		var got []byte
		for i, msg := range msgs[1 : len(msgs)-1] {
			raw, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil || msg.Type != "snapshot_chunk" || msg.Seq != uint32(i+1) ||
				len(raw) == 0 || len(raw) > chunk {
				t.Fatalf("size %d: chunk %d = %s seq %d len %d err %v", size, i, msg.Type, msg.Seq, len(raw), err)
			}
			got = append(got, raw...)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("size %d: reassembled %d bytes differ", size, len(got))
		}
	}
}

func TestSendSnapshotStopsWhenQueueCloses(t *testing.T) {
	var sent int
	sendSnapshot("s1", "c1", make([]byte, 3*protocol.SnapshotChunkBytes), func(protocol.AgentMsg) bool {
		sent++
		return sent < 2
	})
	if sent != 2 {
		t.Fatalf("enqueue calls = %d, want 2 (start and the failed first chunk)", sent)
	}
}

func TestClientAttachRequestsSnapshotForTheClient(t *testing.T) {
	var gotSession, gotClient string
	handleServerMsg(
		protocol.ServerMsg{Type: "client_attach", SessionID: "missing", ClientID: "c9"},
		&serverMsgEnv{
			reg: sessions.New(), modes: map[string]sessions.SessionMode{}, lazyMap: map[string]*lazySession{},
			send:            func(protocol.AgentMsg) { t.Fatal("client_attach sent directly instead of via requestSnapshot") },
			requestSnapshot: func(sessionID, clientID string) { gotSession, gotClient = sessionID, clientID },
		},
	)
	if gotSession != "missing" || gotClient != "c9" {
		t.Fatalf("requestSnapshot(%q, %q)", gotSession, gotClient)
	}
}

// TestFailedActivationEndsTheAttachPromptly: when a lazy session cannot be
// activated, the attaching client gets an empty snapshot and the session is
// removed (the relay closes its clients with "session ended") and listed
// again as inactive, instead of the client waiting for the snapshot timeout.
func TestFailedActivationEndsTheAttachPromptly(t *testing.T) {
	var sent []protocol.AgentMsg
	var readded []string
	lazy := map[string]*lazySession{"lazy": {
		id: "lazy", cmd: "tmux:work", mode: sessions.ModeReadonly,
		activate: func(uint16, uint16) error { return fmt.Errorf("tmux attach failed") },
	}}
	handleServerMsg(
		protocol.ServerMsg{Type: "client_attach", SessionID: "lazy", ClientID: "c1"},
		&serverMsgEnv{
			reg: sessions.New(), modes: map[string]sessions.SessionMode{}, lazyMap: lazy,
			send: func(msg protocol.AgentMsg) { sent = append(sent, msg) },
			sendSessionAdd: func(id, cmd string, _ time.Time, _ sessions.SessionMode, active bool, _, _ uint16) {
				if active {
					t.Errorf("failed session re-advertised as active")
				}
				readded = append(readded, id)
			},
			requestSnapshot: func(string, string) { t.Error("snapshot requested for a failed activation") },
		},
	)
	var types []string
	for _, msg := range sent {
		types = append(types, msg.Type)
		if msg.SessionID != "lazy" || (msg.Type != "session_remove" && msg.ClientID != "c1") || msg.Total != 0 || msg.Seq != 0 {
			t.Fatalf("unexpected message %+v", msg)
		}
	}
	if strings.Join(types, ",") != "snapshot_start,snapshot_end,session_remove" {
		t.Fatalf("sent %v", types)
	}
	if len(readded) != 1 || readded[0] != "lazy" {
		t.Fatalf("re-advertised %v", readded)
	}
	if lazy["lazy"] == nil {
		t.Fatal("failed lazy session was dropped and cannot be retried")
	}
}
