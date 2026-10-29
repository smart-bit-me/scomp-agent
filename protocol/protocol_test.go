package protocol

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAgentMsg_Roundtrip(t *testing.T) {
	orig := AgentMsg{
		Type:      "session_add",
		UID:       "abc123",
		SessionID: "sess1",
		Data:      "base64data",
		Info: &SessionInfo{
			ID:      "sess1",
			Cmd:     "/bin/bash",
			Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Mode:    ModeFull,
		},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got AgentMsg
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != orig.Type || got.UID != orig.UID || got.SessionID != orig.SessionID {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if got.Info == nil || got.Info.ID != orig.Info.ID || got.Info.Mode != orig.Info.Mode {
		t.Fatalf("info mismatch: %+v", got.Info)
	}
}

func TestServerMsg_Roundtrip(t *testing.T) {
	orig := ServerMsg{
		Type:      "resize",
		SessionID: "sess1",
		Cols:      80,
		Rows:      24,
	}
	b, _ := json.Marshal(orig)
	var got ServerMsg
	json.Unmarshal(b, &got)
	if got.Cols != 80 || got.Rows != 24 || got.SessionID != "sess1" {
		t.Fatalf("got %+v", got)
	}
}

func TestAgentMsg_OmitemptyInput(t *testing.T) {
	msg := AgentMsg{Type: "hello", UID: "abc"}
	b, _ := json.Marshal(msg)
	s := string(b)
	for _, field := range []string{"session_id", "client_id", "data", "info"} {
		if contains(s, field) {
			t.Errorf("omitempty field %q present in %s", field, s)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}

func TestSnapshotChunkFieldsRoundtrip(t *testing.T) {
	for _, orig := range []AgentMsg{
		{Type: "snapshot_start", SessionID: "s", ClientID: "c1", Total: 1 << 20},
		{Type: "snapshot_chunk", SessionID: "s", ClientID: "c1", Seq: 22, Data: "AAAA"},
		{Type: "snapshot_end", SessionID: "s", ClientID: "c1", Seq: 22},
	} {
		b, err := json.Marshal(orig)
		if err != nil {
			t.Fatal(err)
		}
		var got AgentMsg
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if got != orig {
			t.Fatalf("roundtrip %s = %+v, want %+v", orig.Type, got, orig)
		}
	}
}

func TestMaxSnapshotChunkStaysNear64KiB(t *testing.T) {
	b, err := json.Marshal(AgentMsg{
		Type:      "snapshot_chunk",
		SessionID: strings.Repeat("s", 128),
		ClientID:  strings.Repeat("c", 128),
		Seq:       ^uint32(0),
		Data:      base64.StdEncoding.EncodeToString(make([]byte, SnapshotChunkBytes)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 66<<10 {
		t.Fatalf("max snapshot_chunk = %d bytes, want <= 66 KiB", len(b))
	}
}
