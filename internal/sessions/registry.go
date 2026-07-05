package sessions

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/smart-bit-me/scomp-agent/internal/pty"
)

// SessionMode controls what input is permitted for a session.
type SessionMode string

const (
	ModeFull     SessionMode = "full"
	ModeReadonly SessionMode = "readonly"
	ModeApproved SessionMode = "approved-only"
)

type Info struct {
	ID      string      `json:"id"`
	Cmd     string      `json:"cmd"`
	Created time.Time   `json:"created"`
	Mode    SessionMode `json:"mode,omitempty"`
}

type entry struct {
	Info
	session *pty.Session
	source  string // dedup key; empty for plain spawned sessions
}

type Registry struct {
	mu         sync.RWMutex
	entries    map[string]*entry
	sourceToID map[string]string // source key → session id
}

func New() *Registry {
	return &Registry{
		entries:    make(map[string]*entry),
		sourceToID: make(map[string]string),
	}
}

func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// sourceKey returns a dedup key for well-known multiplexer attach commands.
// Returns "" for plain sessions (no dedup).
func sourceKey(cmd string, args []string) string {
	base := cmd
	if i := strings.LastIndex(cmd, "/"); i >= 0 {
		base = cmd[i+1:]
	}
	switch base {
	case "tmux":
		// tmux attach-session -t <name>
		if len(args) >= 3 && args[0] == "attach-session" && args[1] == "-t" {
			return "tmux:" + args[2]
		}
		// tmux new-session -A -t <target> -s <linked-name>
		if len(args) > 0 && args[0] == "new-session" {
			for i, a := range args {
				if a == "-s" && i+1 < len(args) {
					return "tmux:" + args[i+1]
				}
			}
		}
	case "screen":
		// screen -x <session>
		if len(args) >= 2 && args[0] == "-x" {
			return "screen:" + args[1]
		}
	}
	return ""
}

func (r *Registry) Create(cmd string, args []string, cols, rows uint16) (Info, *pty.Session, error) {
	src := sourceKey(cmd, args)

	if src != "" {
		r.mu.RLock()
		if id, ok := r.sourceToID[src]; ok {
			if e, ok := r.entries[id]; ok {
				info, sess := e.Info, e.session
				r.mu.RUnlock()
				return info, sess, nil
			}
		}
		r.mu.RUnlock()
	}

	sess, err := pty.New(cmd, args, cols, rows)
	if err != nil {
		return Info{}, nil, err
	}
	id := newID()
	info := Info{ID: id, Cmd: cmd, Created: time.Now()}

	r.mu.Lock()
	r.entries[id] = &entry{Info: info, session: sess, source: src}
	if src != "" {
		r.sourceToID[src] = id
	}
	r.mu.Unlock()

	go func() {
		<-sess.Done()
		r.mu.Lock()
		delete(r.entries, id)
		if src != "" {
			delete(r.sourceToID, src)
		}
		r.mu.Unlock()
	}()

	return info, sess, nil
}

func (r *Registry) Get(id string) (*pty.Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return nil, false
	}
	return e.session, true
}

func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Info, 0, len(r.entries))
	for _, e := range r.entries {
		result = append(result, e.Info)
	}
	return result
}

// Snapshot returns the ring buffer contents for a session (for replay on reconnect).
func (r *Registry) Snapshot(id string) []byte {
	r.mu.RLock()
	e, ok := r.entries[id]
	r.mu.RUnlock()
	if !ok {
		return nil
	}
	return e.session.Snapshot()
}

// CreateWithID creates a PTY session using a caller-supplied ID.
func (r *Registry) CreateWithID(id, cmd string, args []string, cols, rows uint16) (Info, *pty.Session, error) {
	src := sourceKey(cmd, args)

	if src != "" {
		r.mu.RLock()
		if existingID, ok := r.sourceToID[src]; ok {
			if e, ok := r.entries[existingID]; ok {
				info, sess := e.Info, e.session
				r.mu.RUnlock()
				// Register the requested id as an alias so reg.Get(id) succeeds
				// for startForwarder — without this, the forwarder never starts.
				r.mu.Lock()
				r.entries[id] = &entry{Info: Info{ID: id, Cmd: info.Cmd, Created: info.Created}, session: sess}
				r.mu.Unlock()
				go func() {
					<-sess.Done()
					r.mu.Lock()
					delete(r.entries, id)
					r.mu.Unlock()
				}()
				return info, sess, nil
			}
		}
		r.mu.RUnlock()
	}

	sess, err := pty.New(cmd, args, cols, rows)
	if err != nil {
		return Info{}, nil, err
	}
	info := Info{ID: id, Cmd: cmd, Created: time.Now()}

	r.mu.Lock()
	r.entries[id] = &entry{Info: info, session: sess, source: src}
	if src != "" {
		r.sourceToID[src] = id
	}
	r.mu.Unlock()

	go func() {
		<-sess.Done()
		r.mu.Lock()
		delete(r.entries, id)
		if src != "" {
			delete(r.sourceToID, src)
		}
		r.mu.Unlock()
	}()

	return info, sess, nil
}

func (r *Registry) Remove(id string) {
	r.mu.Lock()
	e, ok := r.entries[id]
	if ok {
		delete(r.entries, id)
		if e.source != "" {
			delete(r.sourceToID, e.source)
		}
	}
	r.mu.Unlock()
	if ok {
		e.session.Close()
	}
}
