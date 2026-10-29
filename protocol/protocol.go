package protocol

import "time"

// SessionMode controls what input is allowed for a session.
type SessionMode string

const (
	ModeFull     SessionMode = "full"
	ModeReadonly SessionMode = "readonly"
	ModeApproved SessionMode = "approved-only"
)

// SessionInfo is the session metadata shared between agent and server.
type SessionInfo struct {
	ID      string      `json:"id"`
	Cmd     string      `json:"cmd"`
	Created time.Time   `json:"created"`
	Mode    SessionMode `json:"mode,omitempty"`
	Active  bool        `json:"active,omitempty"` // true once the PTY is running
	Cols    uint16      `json:"cols,omitempty"`
	Rows    uint16      `json:"rows,omitempty"`
}

// SnapshotChunkBytes is the maximum raw payload of one snapshot_chunk. After
// base64 (4/3) and the JSON envelope it stays near 64 KiB, far below every
// WebSocket read limit on the agent-relay path.
const SnapshotChunkBytes = 48 << 10

// AgentMsg is a message sent from the scomp agent to scomp-server.
//
// type values:
//   - "hello"          — initial handshake (uid, version, hostname, pub_key)
//   - "auth"           — challenge response (sig): Ed25519 signature over the
//     server's nonce, proving possession of the agent's private key (F3)
//   - "session_add"    — new PTY session available (info)
//   - "session_remove" — session ended (session_id)
//   - "output"         — PTY bytes for a session (session_id, data base64)
//   - "snapshot"       — legacy single-message ring buffer reply (session_id,
//     client_id, data base64); still accepted by the relay, no longer sent
//   - "snapshot_start" — begins the attach replay for one client (session_id,
//     client_id, total = raw snapshot bytes; 0 means an empty snapshot)
//   - "snapshot_chunk" — ordered replay bytes (session_id, client_id, seq,
//     data base64); seq starts at 1 and increments by one, and each chunk
//     carries 1..SnapshotChunkBytes raw bytes
//   - "snapshot_end"   — completes the replay (session_id, client_id, seq =
//     the final chunk's seq, 0 for an empty snapshot)
//   - "pair_submit"    — reply to a pair_request with the PIN typed at the agent's
//     terminal (pair_id, pin) — proof the user controls the host
//
// Attach replay ordering: for each session the agent emits snapshot_start,
// the chunks, and snapshot_end as one uninterrupted batch at a single point
// of that session's output stream. The snapshot holds exactly the output
// emitted before snapshot_start (bounded by the ring size), and every
// "output" message for the session after snapshot_end follows it with no gap
// or overlap. The relay therefore drops a client's live output until its
// snapshot_start and forwards everything after it in order.
type AgentMsg struct {
	Type      string       `json:"type"`
	UID       string       `json:"uid,omitempty"`
	Version   string       `json:"version,omitempty"`
	Hostname  string       `json:"hostname,omitempty"`
	PubKey    string       `json:"pub_key,omitempty"` // hex Ed25519 public key (hello)
	Sig       string       `json:"sig,omitempty"`     // hex Ed25519 signature over the nonce (auth)
	SessionID string       `json:"session_id,omitempty"`
	ClientID  string       `json:"client_id,omitempty"`
	Data      string       `json:"data,omitempty"`  // base64-encoded bytes
	Seq       uint32       `json:"seq,omitempty"`   // snapshot_chunk / snapshot_end sequence
	Total     int          `json:"total,omitempty"` // snapshot_start raw byte count
	Info      *SessionInfo `json:"info,omitempty"`
	PairID    string       `json:"pair_id,omitempty"`
	PIN       string       `json:"pin,omitempty"`
}

// ServerMsg is a message sent from scomp-server to the scomp agent.
//
// type values:
//   - "challenge"      — proof-of-possession challenge: sign nonce with the agent
//     private key and reply with an "auth" AgentMsg (F3)
//   - "input"          — keyboard bytes for a session (session_id, data base64)
//   - "resize"         — terminal resize (session_id, cols, rows)
//   - "client_attach"  — browser just connected to session (session_id, client_id,
//     cols, rows); the agent answers with a snapshot batch. cols/rows only
//     size a lazy session's new PTY and are 0 for read-only clients; an
//     attach never resizes an existing PTY
//   - "client_detach"  — browser disconnected from session (session_id, client_id)
//   - "pair_request"   — ask the agent to prompt for a pairing PIN at its terminal
//     (pair_id); the user types the PIN shown in their browser
type ServerMsg struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
	Data      string `json:"data,omitempty"` // base64-encoded bytes
	Cols      uint16 `json:"cols,omitempty"`
	Rows      uint16 `json:"rows,omitempty"`
	PairID    string `json:"pair_id,omitempty"`
	Nonce     string `json:"nonce,omitempty"` // hex challenge nonce
}
