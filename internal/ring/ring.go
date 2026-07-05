package ring

import "sync"

// DefaultSize is the default ring buffer capacity (1 MiB).
const DefaultSize = 1 << 20

// Buffer is a thread-safe circular byte buffer.
type Buffer struct {
	mu   sync.Mutex
	buf  []byte
	head int  // next write position
	full bool // true once the buffer has wrapped at least once
}

// New returns a Buffer with the given capacity.
func New(size int) *Buffer {
	return &Buffer{buf: make([]byte, size)}
}

// Write appends p, overwriting the oldest data when the buffer is full.
func (b *Buffer) Write(p []byte) {
	if len(p) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	size := len(b.buf)
	if size == 0 {
		return
	}

	if len(p) >= size {
		// p larger than ring: only keep the last size bytes.
		copy(b.buf, p[len(p)-size:])
		b.head = 0
		b.full = true
		return
	}

	tail := size - b.head
	if len(p) <= tail {
		copy(b.buf[b.head:], p)
		b.head += len(p)
		if b.head == size {
			b.head = 0
			b.full = true
		}
	} else {
		// Wraps around.
		n := copy(b.buf[b.head:], p)
		copy(b.buf, p[n:])
		b.head = len(p) - n
		b.full = true
	}
}

// Snapshot returns all buffered data in chronological order (oldest → newest).
// Returns nil if the buffer is empty.
func (b *Buffer) Snapshot() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.head == 0 && !b.full {
		return nil
	}
	if !b.full {
		out := make([]byte, b.head)
		copy(out, b.buf[:b.head])
		return out
	}
	// Full ring: data goes from head to end of buf, then 0 to head.
	out := make([]byte, len(b.buf))
	n := copy(out, b.buf[b.head:])
	copy(out[n:], b.buf[:b.head])
	return out
}
