package main

import (
	"log"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/smart-bit-me/scomp-agent/internal/screensess"
	"github.com/smart-bit-me/scomp-agent/internal/sessions"
	"github.com/smart-bit-me/scomp-agent/internal/tmuxsess"
)

type sessionChange struct {
	add      *lazySession
	removeID string
}

// sessionCatalog keeps stable IDs for discovered multiplexer sessions and
// publishes lifecycle changes to the currently connected relay client.
type sessionCatalog struct {
	mu              sync.Mutex
	reg             *sessions.Registry
	mode            sessions.SessionMode
	onTmuxActivated func(string)
	entries         map[string]lazySession
	subscribers     map[chan sessionChange]struct{}
}

func newSessionCatalog(
	reg *sessions.Registry,
	mode sessions.SessionMode,
	onTmuxActivated func(string),
) *sessionCatalog {
	c := &sessionCatalog{
		reg:             reg,
		mode:            mode,
		onTmuxActivated: onTmuxActivated,
		entries:         make(map[string]lazySession),
		subscribers:     make(map[chan sessionChange]struct{}),
	}
	c.scan(false)
	return c
}

// scan reconciles the catalog against tmux and screen. Calls are event-driven
// by fsnotify and tmux control mode; there is no timer or periodic polling.
func (c *sessionCatalog) scan(notify bool) {
	tmuxNow := tmuxsess.List()
	screenNow := screensess.List()
	present := make(map[string]bool, len(tmuxNow)+len(screenNow))

	c.mu.Lock()
	var changes []sessionChange
	for _, session := range tmuxNow {
		key := "tmux:" + session.Name
		present[key] = true
		if _, exists := c.entries[key]; exists {
			continue
		}
		name := session.Name
		id := newSessionID()
		entry := lazySession{
			id:      id,
			cmd:     key,
			created: time.Now(),
			mode:    c.mode,
			activate: func(cols, rows uint16) error {
				cmd, args := tmuxsess.LinkedSessionArgs(name)
				_, _, err := c.reg.CreateWithID(id, cmd, args, cols, rows)
				if err == nil {
					c.onTmuxActivated(name)
				}
				return err
			},
		}
		c.entries[key] = entry
		entryCopy := entry
		changes = append(changes, sessionChange{add: &entryCopy})
		log.Printf("sesswatch: new tmux session: %s", name)
	}

	for _, session := range screenNow {
		key := "screen:" + session.Name
		present[key] = true
		if _, exists := c.entries[key]; exists {
			continue
		}
		fullName := session.FullName
		shortName := session.Name
		id := newSessionID()
		entry := lazySession{
			id:      id,
			cmd:     key,
			created: time.Now(),
			mode:    c.mode,
			activate: func(cols, rows uint16) error {
				cmd, args := screensess.AttachArgs(fullName)
				_, _, err := c.reg.CreateWithID(id, cmd, args, cols, rows)
				return err
			},
		}
		c.entries[key] = entry
		entryCopy := entry
		changes = append(changes, sessionChange{add: &entryCopy})
		log.Printf("sesswatch: new screen session: %s", shortName)
	}

	var removed []lazySession
	for key, entry := range c.entries {
		if present[key] {
			continue
		}
		delete(c.entries, key)
		removed = append(removed, entry)
		changes = append(changes, sessionChange{removeID: entry.id})
		log.Printf("sesswatch: removed session: %s", key)
	}

	if notify {
		for subscriber := range c.subscribers {
			for _, change := range changes {
				select {
				case subscriber <- change:
				default:
					// A reconnect obtains an authoritative snapshot. Session
					// lifecycle bursts are otherwise coalesced by the buffer.
				}
			}
		}
	}
	empty := len(c.entries) == 0
	c.mu.Unlock()

	for _, entry := range removed {
		if _, active := c.reg.Get(entry.id); !active {
			continue
		}
		c.reg.Remove(entry.id)
		if strings.HasPrefix(entry.cmd, "tmux:") {
			_ = exec.Command("tmux", "kill-session", "-t", "scomp-"+entry.cmd[len("tmux:"):]).Run()
		}
	}

	if empty {
		log.Printf("no tmux or screen sessions found — start one and the agent will detect it automatically")
	}
}

// snapshotAndSubscribe atomically returns the current catalog and registers a
// listener, so a session change cannot be lost between the two operations.
func (c *sessionCatalog) snapshotAndSubscribe() ([]lazySession, <-chan sessionChange, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := make([]lazySession, 0, len(c.entries))
	for _, entry := range c.entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].cmd < result[j].cmd })

	changes := make(chan sessionChange, 128)
	c.subscribers[changes] = struct{}{}
	unsubscribe := func() {
		c.mu.Lock()
		delete(c.subscribers, changes)
		c.mu.Unlock()
	}
	return result, changes, unsubscribe
}
