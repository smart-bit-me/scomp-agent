package pty

import "testing"

func TestTransformInputFollowsDECCKM(t *testing.T) {
	var tr modeTracker
	if got := string(tr.transformInput([]byte("\x1b[A"))); got != "\x1b[A" {
		t.Fatalf("normal mode: %q", got)
	}
	tr.process([]byte("\x1b[?1h"))
	if got := string(tr.transformInput([]byte("\x1b[A"))); got != "\x1bOA" {
		t.Fatalf("application mode: %q", got)
	}
	tr.process([]byte("\x1b[?1l"))
	if tr.DECCKM {
		t.Fatal("DECCKM still set after CSI ? 1 l")
	}
}
