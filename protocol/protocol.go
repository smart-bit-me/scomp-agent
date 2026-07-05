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

// AgentMsg is a message sent from the scomp agent to scomp-server.
//
// type values:
//   - "hello"          — initial handshake (uid, version, hostname)
//   - "session_add"    — new PTY session available (info)
//   - "session_remove" — session ended (session_id)
//   - "output"         — PTY bytes for a session (session_id, data base64)
//   - "snapshot"       — ring buffer reply (session_id, client_id, data base64)
//   - "pair_submit"    — reply to a pair_request with the PIN typed at the agent's
//     terminal (pair_id, pin) — proof the user controls the host
type AgentMsg struct {
	Type      string       `json:"type"`
	UID       string       `json:"uid,omitempty"`
	Version   string       `json:"version,omitempty"`
	Hostname  string       `json:"hostname,omitempty"`
	SessionID string       `json:"session_id,omitempty"`
	ClientID  string       `json:"client_id,omitempty"`
	Data      string       `json:"data,omitempty"` // base64-encoded bytes
	Info      *SessionInfo `json:"info,omitempty"`
	PairID    string       `json:"pair_id,omitempty"`
	PIN       string       `json:"pin,omitempty"`
}

// ServerMsg is a message sent from scomp-server to the scomp agent.
//
// type values:
//   - "input"          — keyboard bytes for a session (session_id, data base64)
//   - "resize"         — terminal resize (session_id, cols, rows)
//   - "client_attach"  — browser just connected to session (session_id, client_id)
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
}
