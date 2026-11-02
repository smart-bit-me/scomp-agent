package sessions

import (
	"sync"
	"time"

	"github.com/smart-bit-me/scomp-agent/internal/stream"
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

// Terminal is a running session: a PTY-backed process or an in-process fake
// terminal. Output is delivered through the embedded stream semantics.
type Terminal interface {
	Write(data []byte) error
	Resize(cols, rows uint16) error
	Subscribe(ch chan<- stream.Event, overflow func())
	Unsubscribe(ch chan<- stream.Event)
	RequestSnapshot(ch chan<- stream.Event, clientID string) bool
	Snapshot() []byte
	Done() <-chan struct{}
	Close()
}

type entry struct {
	Info
	session Terminal
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

// alive reports whether a session's PTY is still running. A session is
// marked done before forgetWhenDone removes it from the maps, so lookups
// check this instead of trusting map presence.
func alive(sess Terminal) bool {
	select {
	case <-sess.Done():
		return false
	default:
		return true
	}
}

// forgetWhenDone removes id once sess ends, but only while id still refers
// to sess: a lazy session reactivated under the same id must not lose its
// new entry to the old session's late cleanup.
func (r *Registry) forgetWhenDone(id string, sess Terminal) {
	<-sess.Done()
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok || e.session != sess {
		return
	}
	delete(r.entries, id)
	if e.source != "" && r.sourceToID[e.source] == id {
		delete(r.sourceToID, e.source)
	}
}

// Get returns a running session. A session whose PTY has ended is reported
// as missing even before its cleanup runs.
func (r *Registry) Get(id string) (Terminal, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok || !alive(e.session) {
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

// Add registers an already running terminal under id. An existing entry with
// the same id is replaced (and closed).
func (r *Registry) Add(id, cmd string, term Terminal) Info {
	info := Info{ID: id, Cmd: cmd, Created: time.Now()}
	r.mu.Lock()
	old, existed := r.entries[id]
	r.entries[id] = &entry{Info: info, session: term}
	r.mu.Unlock()
	if existed && old.session != term {
		old.session.Close()
	}
	go r.forgetWhenDone(id, term)
	return info
}

func (r *Registry) Remove(id string) {
	r.mu.Lock()
	e, ok := r.entries[id]
	if ok {
		delete(r.entries, id)
		if e.source != "" && r.sourceToID[e.source] == id {
			delete(r.sourceToID, e.source)
		}
	}
	r.mu.Unlock()
	if ok {
		e.session.Close()
	}
}
