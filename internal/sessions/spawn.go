//go:build !fakeshellonly

package sessions

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/smart-bit-me/scomp-agent/internal/pty"
)

// PTY spawning lives in this file so that a fakeshellonly build links no
// process-spawning code (see docs/FAKESHELL.md).

func (r *Registry) Create(cmd string, args []string, cols, rows uint16) (Info, Terminal, error) {
	src := sourceKey(cmd, args)

	if src != "" {
		r.mu.RLock()
		if e, ok := r.liveSourceLocked(src); ok {
			info, sess := e.Info, e.session
			r.mu.RUnlock()
			return info, sess, nil
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
	go r.forgetWhenDone(id, sess)

	return info, sess, nil
}

// CreateWithID creates a PTY session using a caller-supplied ID.
func (r *Registry) CreateWithID(id, cmd string, args []string, cols, rows uint16) (Info, Terminal, error) {
	src := sourceKey(cmd, args)

	if src != "" {
		r.mu.Lock()
		if e, ok := r.liveSourceLocked(src); ok {
			info, sess := e.Info, e.session
			// Register the requested id as an alias so reg.Get(id) succeeds
			// for startForwarder — without this, the forwarder never starts.
			r.entries[id] = &entry{Info: Info{ID: id, Cmd: info.Cmd, Created: info.Created}, session: sess}
			r.mu.Unlock()
			go r.forgetWhenDone(id, sess)
			return info, sess, nil
		}
		r.mu.Unlock()
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
	go r.forgetWhenDone(id, sess)

	return info, sess, nil
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
		// tmux attach-session -t [=]<name>
		if len(args) >= 3 && args[0] == "attach-session" && args[1] == "-t" {
			return "tmux:" + strings.TrimPrefix(args[2], "=")
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

func (r *Registry) liveSourceLocked(src string) (*entry, bool) {
	id, ok := r.sourceToID[src]
	if !ok {
		return nil, false
	}
	e, ok := r.entries[id]
	if !ok || !alive(e.session) {
		return nil, false
	}
	return e, true
}
