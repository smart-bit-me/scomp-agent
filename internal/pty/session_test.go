package pty

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/smart-bit-me/scomp-agent/internal/ring"
)

func TestBroadcastSignalsSubscriberOverflow(t *testing.T) {
	s := &Session{subs: make(map[chan<- Event]func())}
	full := make(chan Event, 1)
	full <- Event{Data: []byte("already queued")}
	var overflows atomic.Int32
	s.Subscribe(full, func() { overflows.Add(1) })

	s.broadcast([]byte("would be dropped"))

	if got := overflows.Load(); got != 1 {
		t.Fatalf("overflow callbacks = %d, want 1", got)
	}
	if got := string((<-full).Data); got != "already queued" {
		t.Fatalf("queued frame = %q", got)
	}
}

func TestBroadcastDeliversToAvailableSubscriber(t *testing.T) {
	s := &Session{subs: make(map[chan<- Event]func())}
	available := make(chan Event, 1)
	var overflows atomic.Int32
	s.Subscribe(available, func() { overflows.Add(1) })

	s.broadcast([]byte("frame"))

	if got := string((<-available).Data); got != "frame" {
		t.Fatalf("delivered frame = %q", got)
	}
	if got := overflows.Load(); got != 0 {
		t.Fatalf("overflow callbacks = %d, want 0", got)
	}
}

// TestSnapshotIsExactlyThePrecedingStream checks the attach ordering
// invariant: with output racing snapshot requests, each snapshot event equals
// the concatenation of every live event delivered before it on the same
// subscriber stream, so snapshot + following live output never gaps or
// duplicates bytes.
func TestSnapshotIsExactlyThePrecedingStream(t *testing.T) {
	s := &Session{ring: ring.New(1 << 20), subs: make(map[chan<- Event]func())}
	events := make(chan Event, 1<<14)
	s.Subscribe(events, func() { t.Error("subscriber overflow") })

	const writes, snapshots = 2000, 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < writes; i++ {
			s.emit([]byte(fmt.Sprintf("<%d>", i)))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < snapshots; i++ {
			if !s.RequestSnapshot(events, fmt.Sprintf("c%d", i)) {
				t.Error("snapshot request rejected")
			}
		}
	}()
	wg.Wait()
	close(events)

	var stream bytes.Buffer
	seen := 0
	for ev := range events {
		if !ev.Snapshot {
			stream.Write(ev.Data)
			continue
		}
		seen++
		if !bytes.Equal(ev.Data, stream.Bytes()) {
			t.Fatalf("snapshot %s has %d bytes, preceding stream has %d", ev.ClientID, len(ev.Data), stream.Len())
		}
	}
	if seen != snapshots {
		t.Fatalf("snapshots = %d, want %d", seen, snapshots)
	}
}

func TestRequestSnapshotReportsFullSubscriber(t *testing.T) {
	s := &Session{ring: ring.New(16), subs: make(map[chan<- Event]func())}
	full := make(chan Event, 1)
	full <- Event{}
	if s.RequestSnapshot(full, "c1") {
		t.Fatal("snapshot queued on a full subscriber channel")
	}
}
