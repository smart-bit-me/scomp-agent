package main

import (
	"bytes"
	"encoding/base64"
	"testing"

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
		sessions.New(), map[string]sessions.SessionMode{}, map[string]*lazySession{},
		func(protocol.AgentMsg) { t.Fatal("client_attach sent directly instead of via requestSnapshot") },
		nil, nil,
		func(sessionID, clientID string) { gotSession, gotClient = sessionID, clientID },
	)
	if gotSession != "missing" || gotClient != "c9" {
		t.Fatalf("requestSnapshot(%q, %q)", gotSession, gotClient)
	}
}
