package fakeshell

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/smart-bit-me/scomp-agent/internal/stream"
)

// collect gathers a session's live output.
func collect(s *Session) (*capture, func()) {
	out := &capture{}
	ch := make(chan stream.Event, 1024)
	s.Subscribe(ch, nil)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case ev := <-ch:
				out.emit(ev.Data, false)
			}
		}
	}()
	return out, func() { s.Unsubscribe(ch); close(stop) }
}

func program(name string) Program {
	for _, p := range Programs() {
		if p.Name == name {
			return p
		}
	}
	panic("no program " + name)
}

func TestSessionShellRoundTrip(t *testing.T) {
	s := NewSession(program("shell"), Config{}, 80, 24)
	defer s.Close()
	out, stop := collect(s)
	defer stop()
	waitFor(t, out, "$ ")
	s.Write([]byte("echo round-trip-ok\r"))
	waitFor(t, out, "round-trip-ok\n")
	// Arrow keys in both encodings recall history.
	s.Write([]byte("\x1bOA\r"))
	waitFor(t, out, "round-trip-ok\n")
	s.Write([]byte("\x1b[A\r"))
	waitFor(t, out, "round-trip-ok\n")
}

func TestSessionClearTrimsTheReplay(t *testing.T) {
	s := NewSession(program("shell"), Config{}, 80, 24)
	defer s.Close()
	out, stop := collect(s)
	defer stop()
	waitFor(t, out, "$ ")
	s.Write([]byte("echo before-clear\r"))
	waitFor(t, out, "before-clear\n")
	s.Write([]byte("clear\r"))
	waitFor(t, out, "$ ")
	time.Sleep(50 * time.Millisecond)
	if snap := string(s.Snapshot()); strings.Contains(snap, "before-clear") || strings.Contains(snap, "Welcome") {
		t.Errorf("snapshot after clear still holds old output: %q", snap)
	}
}

func TestSessionIdleResetStartsFresh(t *testing.T) {
	s := NewSession(program("shell"), Config{IdleReset: 150 * time.Millisecond}, 80, 24)
	defer s.Close()
	out, stop := collect(s)
	defer stop()
	waitFor(t, out, "$ ")
	s.Write([]byte("touch reviewer-was-here\r"))
	s.Write([]byte("cd app\r"))
	waitFor(t, out, "~/app$ ")
	waitFor(t, out, "Welcome to review-host")
	s.Write([]byte("ls reviewer-was-here\r"))
	waitFor(t, out, "No such file")
	s.Write([]byte("pwd\r"))
	waitFor(t, out, "\n/home/review\n")
}

func TestSessionRecoversFromPanic(t *testing.T) {
	calls := 0
	boom := Program{Name: "boom", run: func(ctx context.Context, t *TTY, cfg Config) {
		calls++
		if calls == 1 {
			panic("test panic")
		}
		t.WriteString("second life\n")
		<-ctx.Done()
	}}
	s := NewSession(boom, Config{}, 80, 24)
	defer s.Close()
	out, stop := collect(s)
	defer stop()
	waitFor(t, out, "second life")
}

func TestSessionWriteNeverBlocks(t *testing.T) {
	s := NewSession(program("logs"), Config{}, 80, 24) // logs never reads its keys quickly
	defer s.Close()
	done := make(chan struct{})
	go func() {
		big := []byte(strings.Repeat("x", 4*keyQueue))
		for i := 0; i < 10; i++ {
			s.Write(big)
		}
		s.Write(make([]byte, 2*maxInputBytes))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Write blocked")
	}
}

func TestSessionCloseAndResize(t *testing.T) {
	s := NewSession(program("agent"), Config{}, 0, 0)
	if c, r := s.size(); c != 80 || r != 24 {
		t.Errorf("default size %dx%d", c, r)
	}
	s.Resize(9999, 9999)
	if c, r := s.size(); c != 500 || r != 200 {
		t.Errorf("size not clamped: %dx%d", c, r)
	}
	s.Close()
	s.Close()
	select {
	case <-s.Done():
	default:
		t.Fatal("Done not closed after Close")
	}
}

func TestAgentProgramAnswers(t *testing.T) {
	s := NewSession(program("agent"), Config{}, 80, 24)
	defer s.Close()
	out, stop := collect(s)
	defer stop()
	waitFor(t, out, "Apply this change? (y/n)")
	s.Write([]byte("maybe\r"))
	waitFor(t, out, "please answer y or n")
	s.Write([]byte("y\r"))
	waitFor(t, out, "applied.")
	waitFor(t, out, "Task 2")
	s.Write([]byte("n\r"))
	waitFor(t, out, "skipped.")
}
