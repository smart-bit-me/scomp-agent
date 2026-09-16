package pty

import (
	"sync/atomic"
	"testing"
)

func TestBroadcastSignalsSubscriberOverflow(t *testing.T) {
	s := &Session{subs: make(map[chan<- []byte]func())}
	full := make(chan []byte, 1)
	full <- []byte("already queued")
	var overflows atomic.Int32
	s.Subscribe(full, func() { overflows.Add(1) })

	s.broadcast([]byte("would be dropped"))

	if got := overflows.Load(); got != 1 {
		t.Fatalf("overflow callbacks = %d, want 1", got)
	}
	if got := string(<-full); got != "already queued" {
		t.Fatalf("queued frame = %q", got)
	}
}

func TestBroadcastDeliversToAvailableSubscriber(t *testing.T) {
	s := &Session{subs: make(map[chan<- []byte]func())}
	available := make(chan []byte, 1)
	var overflows atomic.Int32
	s.Subscribe(available, func() { overflows.Add(1) })

	s.broadcast([]byte("frame"))

	if got := string(<-available); got != "frame" {
		t.Fatalf("delivered frame = %q", got)
	}
	if got := overflows.Load(); got != 0 {
		t.Fatalf("overflow callbacks = %d, want 0", got)
	}
}
