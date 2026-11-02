// Package stream is the output side of a terminal session: a bounded ring of
// recent output plus a fan-out to subscribers, with snapshots taken at exact
// points of the stream. Both PTY sessions and in-process fake sessions use it,
// so they share the attach-replay ordering documented in protocol/.
package stream

import (
	"sync"

	"github.com/smart-bit-me/scomp-agent/internal/ring"
)

// Event is one item of a subscriber's ordered output stream: either live
// output or, when Snapshot is set, the ring contents captured for ClientID at
// exactly that point of the stream (see RequestSnapshot).
type Event struct {
	Data     []byte
	Snapshot bool
	ClientID string
}

// Stream records output in a ring and broadcasts it to subscribers.
type Stream struct {
	// mu makes "append to the ring, then broadcast" one atomic step with
	// respect to RequestSnapshot and Reset, so every subscriber stream has a
	// single well-defined position for each snapshot.
	mu   sync.Mutex
	ring *ring.Buffer

	subsMu sync.RWMutex
	subs   map[chan<- Event]func()
}

// New returns a Stream whose ring keeps the last size bytes.
func New(size int) *Stream {
	return &Stream{ring: ring.New(size), subs: make(map[chan<- Event]func())}
}

// Emit records data in the ring and broadcasts it to subscribers.
func (s *Stream) Emit(data []byte) {
	s.mu.Lock()
	s.ring.Write(data)
	s.broadcast(data)
	s.mu.Unlock()
}

// EmitReset discards the ring, then emits data, as one step. Use it when data
// redraws the whole screen: a client attaching later replays only the latest
// frame instead of every frame before it.
func (s *Stream) EmitReset(data []byte) {
	s.mu.Lock()
	s.ring.Reset()
	s.ring.Write(data)
	s.broadcast(data)
	s.mu.Unlock()
}

// broadcast delivers live output to every subscriber. Callers hold mu.
func (s *Stream) broadcast(data []byte) {
	s.subsMu.RLock()
	subs := make(map[chan<- Event]func(), len(s.subs))
	for ch, overflow := range s.subs {
		subs[ch] = overflow
	}
	s.subsMu.RUnlock()
	for ch, overflow := range subs {
		select {
		case ch <- Event{Data: data}:
		default:
			// A missing terminal frame corrupts the subscriber's rendered state.
			// Signal overload so the transport reconnects and obtains a fresh
			// ring-buffer snapshot instead of continuing from a silent gap.
			if overflow != nil {
				overflow()
			}
		}
	}
}

// Subscribe registers ch for live output; overflow runs when ch is full.
func (s *Stream) Subscribe(ch chan<- Event, overflow func()) {
	s.subsMu.Lock()
	s.subs[ch] = overflow
	s.subsMu.Unlock()
}

// Unsubscribe removes ch.
func (s *Stream) Unsubscribe(ch chan<- Event) {
	s.subsMu.Lock()
	delete(s.subs, ch)
	s.subsMu.Unlock()
}

// RequestSnapshot queues on ch, a channel registered with Subscribe, a
// snapshot event for clientID carrying the ring contents at this exact point
// of the output stream. The ring write and the broadcast happen under the same
// lock, so the snapshot holds exactly the output broadcast to ch before the
// event (bounded by the ring size and by the last reset) and none of the
// output broadcast after it. It reports false, without blocking, when ch is
// full.
func (s *Stream) RequestSnapshot(ch chan<- Event, clientID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case ch <- Event{Data: s.ring.Snapshot(), Snapshot: true, ClientID: clientID}:
		return true
	default:
		return false
	}
}

// Snapshot returns the ring buffer contents.
func (s *Stream) Snapshot() []byte {
	return s.ring.Snapshot()
}
