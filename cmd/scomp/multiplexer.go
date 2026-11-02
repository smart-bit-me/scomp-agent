//go:build !fakeshellonly

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync"

	"github.com/smart-bit-me/scomp-agent/internal/sessions"
	"github.com/smart-bit-me/scomp-agent/internal/tmuxsess"
)

// multiplexerSupported reports whether this build can attach real sessions.
const multiplexerSupported = true

// defaultModeFlag is the --mode default of the regular build.
const defaultModeFlag = "readonly"

// startMultiplexerSource discovers tmux and GNU Screen sessions. The returned
// cleanup kills the linked tmux sessions created on activation; it is safe to
// call more than once.
func startMultiplexerSource(ctx context.Context, reg *sessions.Registry, mode sessions.SessionMode) (sessionSource, func(), error) {
	var (
		activatedMu sync.Mutex
		activated   []string
	)
	onTmuxActivated := func(name string) {
		activatedMu.Lock()
		activated = append(activated, name)
		activatedMu.Unlock()
	}
	cleanup := func() {
		activatedMu.Lock()
		names := activated
		activated = nil
		activatedMu.Unlock()
		for _, name := range names {
			linked := tmuxsess.LinkedSessionName(name)
			if err := tmuxsess.KillLinkedSession(name); err != nil {
				log.Printf("kill tmux session %s: %v", linked, err)
			} else {
				log.Printf("killed tmux session %s", linked)
			}
		}
	}
	catalog := newSessionCatalog(reg, mode, onTmuxActivated)
	go watchSessions(ctx, catalog)
	return catalog, cleanup, nil
}

// refresh reconciles the catalog against the actual sockets.
func (c *sessionCatalog) refresh() { c.scan(false) }

func newSessionID() string {
	// 16 bytes = 128 bits: avoids birthday collisions that a 32-bit ID would
	// hit around a few thousand sessions (a collision misroutes input/attach).
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("generating session id: %v", err)
	}
	return hex.EncodeToString(b)
}
