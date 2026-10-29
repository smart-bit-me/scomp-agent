package main

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smart-bit-me/scomp-agent/protocol"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitFor(t *testing.T, out *syncBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("output %q does not contain %q", out.String(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type recordedSends struct {
	mu   sync.Mutex
	msgs []protocol.AgentMsg
}

func (r *recordedSends) sender(ok bool) func(protocol.AgentMsg) bool {
	return func(msg protocol.AgentMsg) bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.msgs = append(r.msgs, msg)
		return ok
	}
}

func (r *recordedSends) all() []protocol.AgentMsg {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]protocol.AgentMsg(nil), r.msgs...)
}

// TestPairPromptSubmitsOnTheCurrentConnection: the prompt outlives the
// connection that requested it, and the PIN goes out on the one that is
// current when it is typed.
func TestPairPromptSubmitsOnTheCurrentConnection(t *testing.T) {
	stdin, typed := io.Pipe()
	defer typed.Close()
	out := &syncBuffer{}
	p := newPairPrompter(stdin, out, time.Minute)
	var old, current recordedSends
	disconnectOld := p.connected(old.sender(true))
	p.request("pair-1")
	waitFor(t, out, "Enter the PIN")
	disconnectOld()
	p.connected(current.sender(true))
	disconnectOld() // a stale teardown must not remove the newer connection
	io.WriteString(typed, "1234\n")
	waitFor(t, out, "PIN submitted")
	if len(old.all()) != 0 {
		t.Fatalf("PIN sent on the old connection: %+v", old.all())
	}
	if got := current.all(); len(got) != 1 || got[0].Type != "pair_submit" || got[0].PairID != "pair-1" || got[0].PIN != "1234" {
		t.Fatalf("current connection got %+v", got)
	}
}

func TestPairPromptReportsWhenNotSubmitted(t *testing.T) {
	stdin, typed := io.Pipe()
	defer typed.Close()
	out := &syncBuffer{}
	p := newPairPrompter(stdin, out, time.Minute)
	p.request("pair-1") // no connection installed
	waitFor(t, out, "Enter the PIN")
	io.WriteString(typed, "1234\n")
	waitFor(t, out, "PIN not submitted")
	if strings.Contains(out.String(), "PIN submitted") {
		t.Fatalf("claimed submission: %q", out.String())
	}

	var refused recordedSends
	p.connected(refused.sender(false)) // e.g. the queue is closing
	p.request("pair-2")
	waitCount(t, out, "Enter the PIN", 2)
	io.WriteString(typed, "5678\n")
	waitCount(t, out, "PIN not submitted", 2)
	if strings.Contains(out.String(), "PIN submitted") {
		t.Fatalf("refused send reported as submitted: %q", out.String())
	}
}

func waitCount(t *testing.T, out *syncBuffer, want string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for strings.Count(out.String(), want) < n {
		if time.Now().After(deadline) {
			t.Fatalf("output %q has fewer than %d of %q", out.String(), n, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestPairPromptExpiresAndReleases: an expired prompt stops waiting, a later
// line is not taken as its PIN, and a new request prompts again.
func TestPairPromptExpiresAndReleases(t *testing.T) {
	stdin, typed := io.Pipe()
	defer typed.Close()
	out := &syncBuffer{}
	p := newPairPrompter(stdin, out, 50*time.Millisecond)
	var sends recordedSends
	p.connected(sends.sender(true))
	p.request("pair-1")
	waitFor(t, out, "pairing request expired")
	io.WriteString(typed, "1234\n") // typed after expiry: discarded
	time.Sleep(100 * time.Millisecond)
	if got := sends.all(); len(got) != 0 {
		t.Fatalf("expired prompt submitted %+v", got)
	}
	p.request("pair-2") // the prompt is free again
	waitCount(t, out, "Enter the PIN", 2)
	waitCount(t, out, "pairing request expired", 2)
}

func TestPairPromptNewerRequestReplacesOlder(t *testing.T) {
	stdin, typed := io.Pipe()
	defer typed.Close()
	out := &syncBuffer{}
	p := newPairPrompter(stdin, out, time.Minute)
	var sends recordedSends
	p.connected(sends.sender(true))
	p.request("pair-1")
	waitFor(t, out, "Enter the PIN")
	p.request("pair-2")
	waitFor(t, out, "replaced the previous one")
	io.WriteString(typed, "1234\n")
	waitFor(t, out, "PIN submitted")
	if got := sends.all(); len(got) != 1 || got[0].PairID != "pair-2" {
		t.Fatalf("sends = %+v", got)
	}
}

func TestPairPromptWithoutStdinAborts(t *testing.T) {
	out := &syncBuffer{}
	p := newPairPrompter(strings.NewReader(""), out, time.Minute)
	var sends recordedSends
	p.connected(sends.sender(true))
	p.request("pair-1")
	waitFor(t, out, "stdin not a terminal")
	p.request("pair-2")
	waitFor(t, out, "run scomp in the foreground to pair")
	if len(sends.all()) != 0 {
		t.Fatalf("sends = %+v", sends.all())
	}
}
