package main

import (
	"sync"
	"testing"
	"time"

	"github.com/smart-bit-me/scomp-agent/internal/screensess"
	"github.com/smart-bit-me/scomp-agent/internal/sessions"
	"github.com/smart-bit-me/scomp-agent/internal/tmuxsess"
)

func newTestCatalog(listTmux func() []tmuxsess.Session) *sessionCatalog {
	return &sessionCatalog{
		reg:             sessions.New(),
		mode:            sessions.ModeReadonly,
		onTmuxActivated: func(string) {},
		entries:         make(map[string]lazySession),
		subscribers:     make(map[chan sessionChange]struct{}),
		listTmux:        listTmux,
		listScreen:      func() []screensess.Session { return nil },
	}
}

// TestConcurrentScanCannotResurrectRemovedSession: a scan that listed a tmux
// session just before it exited must not re-add it after a newer scan
// removed it.
func TestConcurrentScanCannotResurrectRemovedSession(t *testing.T) {
	var (
		mu      sync.Mutex
		calls   int
		release = make(chan struct{})
		listing = make(chan struct{})
	)
	work := []tmuxsess.Session{{Name: "work", Windows: 1}}
	catalog := newTestCatalog(func() []tmuxsess.Session {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		switch call {
		case 1: // initial scan: the session exists
			return work
		case 2: // slow scan: listed before the exit, applies late
			close(listing)
			<-release
			return work
		default: // the session has exited
			return nil
		}
	})
	catalog.scan(false)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); catalog.scan(true) }()
	<-listing
	go func() { defer wg.Done(); catalog.scan(true) }()
	time.Sleep(50 * time.Millisecond) // an unserialized second scan finishes here
	close(release)
	wg.Wait()

	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	if entry, ok := catalog.entries["tmux:work"]; ok {
		t.Fatalf("removed session resurrected as %s", entry.id)
	}
}
