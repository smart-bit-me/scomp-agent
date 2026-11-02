package main

// sessionSource is what runAgent advertises: the tmux/Screen catalog or the
// fixed fake-shell catalog.
type sessionSource interface {
	// refresh reconciles the source with reality after a relay reconnect.
	refresh()
	// snapshotAndSubscribe atomically returns the current sessions and a
	// channel of later changes; the func unsubscribes.
	snapshotAndSubscribe() ([]lazySession, <-chan sessionChange, func())
}

// sessionChange is one lifecycle change published by a sessionSource.
type sessionChange struct {
	add      *lazySession
	removeID string
}
