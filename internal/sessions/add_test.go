package sessions

import (
	"testing"
	"time"

	"github.com/smart-bit-me/scomp-agent/internal/stream"
)

type fakeTerm struct {
	*stream.Stream
	done   chan struct{}
	closed bool
}

func newFakeTerm() *fakeTerm { return &fakeTerm{Stream: stream.New(64), done: make(chan struct{})} }

func (f *fakeTerm) Write([]byte) error          { return nil }
func (f *fakeTerm) Resize(uint16, uint16) error { return nil }
func (f *fakeTerm) Done() <-chan struct{}       { return f.done }
func (f *fakeTerm) Close() {
	if !f.closed {
		f.closed = true
		close(f.done)
	}
}

func TestAddRegistersAndReplacesTerminals(t *testing.T) {
	r := New()
	first := newFakeTerm()
	info := r.Add("id1", "shell", first)
	if info.ID != "id1" || info.Cmd != "shell" {
		t.Fatalf("info = %+v", info)
	}
	if got, ok := r.Get("id1"); !ok || got != first {
		t.Fatal("added terminal not found")
	}
	second := newFakeTerm()
	r.Add("id1", "shell", second)
	if !first.closed {
		t.Error("replaced terminal was not closed")
	}
	if got, _ := r.Get("id1"); got != second {
		t.Error("Get did not return the replacement")
	}
	second.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := r.Get("id1"); !ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("ended terminal still listed")
}
