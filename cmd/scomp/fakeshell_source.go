package main

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/smart-bit-me/scomp-agent/fakeshell"
	"github.com/smart-bit-me/scomp-agent/internal/sessions"
)

// fakeSource advertises the fixed simulated sessions of --mode fakeshell.
// Each starts on its first attach and then lives for the agent's lifetime,
// shared by every viewer, like a tmux session.
type fakeSource struct {
	entries []lazySession
	never   chan sessionChange // never written: the catalog does not change
}

func newFakeSource(reg *sessions.Registry, uid, hostname string) *fakeSource {
	src := &fakeSource{never: make(chan sessionChange)}
	cfg := fakeshell.Config{Hostname: hostname}
	created := time.Now()
	for _, p := range fakeshell.Programs() {
		program := p
		id := fakeSessionID(uid, program.Name)
		src.entries = append(src.entries, lazySession{
			id:      id,
			cmd:     program.Name,
			created: created,
			mode:    sessions.ModeFull,
			activate: func(cols, rows uint16) error {
				reg.Add(id, program.Name, fakeshell.NewSession(program, cfg, cols, rows))
				return nil
			},
		})
	}
	return src
}

// fakeSessionID is stable across restarts, so apps keep their per-session
// state, and distinct per agent identity.
func fakeSessionID(uid, name string) string {
	sum := sha256.Sum256([]byte("scomp-fakeshell\x00" + uid + "\x00" + name))
	return hex.EncodeToString(sum[:16])
}

func (s *fakeSource) refresh() {}

func (s *fakeSource) snapshotAndSubscribe() ([]lazySession, <-chan sessionChange, func()) {
	return append([]lazySession(nil), s.entries...), s.never, func() {}
}
