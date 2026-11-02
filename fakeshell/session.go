// Package fakeshell implements simulated terminal sessions for
// `scomp --mode fakeshell`: a pretend shell, a pretend coding agent and a
// scrolling log. Input is decoded and interpreted in Go; nothing is ever
// passed to the operating system. See docs/FAKESHELL.md.
//
// This package and its subpackages must not import os, os/exec, net,
// syscall, unsafe or plugin (enforced by safety_test.go).
package fakeshell

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/smart-bit-me/scomp-agent/fakeshell/term"
	"github.com/smart-bit-me/scomp-agent/internal/stream"
)

// Config configures the simulated machine.
type Config struct {
	Hostname  string           // shown in the prompt, hostname, uname; default review-host
	Now       func() time.Time // clock; default time.Now
	IdleReset time.Duration    // reset the shell after this long without input; default 15m
	Seed      uint64           // process-table jitter seed; 0 picks one from the clock
}

func (c Config) withDefaults() Config {
	if c.Hostname == "" {
		c.Hostname = "review-host"
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.IdleReset <= 0 {
		c.IdleReset = 15 * time.Minute
	}
	if c.Seed == 0 {
		c.Seed = uint64(c.Now().UnixNano())
	}
	return c
}

// Program is one simulated session.
type Program struct {
	Name string
	run  func(ctx context.Context, t *TTY, cfg Config)
}

// Programs returns the simulated sessions in display order.
func Programs() []Program {
	return []Program{
		{Name: "shell", run: runShell},
		{Name: "agent", run: runAgent},
		{Name: "logs", run: runLogs},
	}
}

// Limits of one session.
const (
	ringSize      = 256 << 10
	keyQueue      = 1024
	maxInputBytes = 64 << 10
	restartDelay  = time.Second
)

// Session is a running Program. It implements sessions.Terminal, so the agent
// relays it exactly like a PTY.
type Session struct {
	*stream.Stream

	keys    chan term.Key
	resized chan struct{}
	decMu   sync.Mutex
	dec     term.Decoder
	cols    atomic.Int32
	rows    atomic.Int32

	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// NewSession starts p with the given window size.
func NewSession(p Program, cfg Config, cols, rows uint16) *Session {
	cfg = cfg.withDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		Stream:  stream.New(ringSize),
		keys:    make(chan term.Key, keyQueue),
		resized: make(chan struct{}, 1),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	s.setSize(cols, rows)
	go s.supervise(ctx, p, cfg)
	return s
}

// supervise runs p until the session is closed. A program that returns or
// panics is restarted after a short delay; it can never take the agent down.
func (s *Session) supervise(ctx context.Context, p Program, cfg Config) {
	t := &TTY{keys: s.keys, resized: s.resized, size: s.size, emit: s.emit, now: cfg.Now, atLineStart: true}
	for ctx.Err() == nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("fakeshell: %s panicked: %v", p.Name, r)
					t.WriteString(fmt.Sprintf("\r\n[%s restarted]\r\n", p.Name))
				}
			}()
			p.run(ctx, t, cfg)
		}()
		select {
		case <-ctx.Done():
		case <-time.After(restartDelay):
		}
	}
}

func (s *Session) emit(p []byte, reset bool) {
	if reset {
		s.EmitReset(p)
	} else {
		s.Emit(p)
	}
}

func (s *Session) setSize(cols, rows uint16) {
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	s.cols.Store(int32(min(cols, 500)))
	s.rows.Store(int32(min(rows, 200)))
}

func (s *Session) size() (int, int) { return int(s.cols.Load()), int(s.rows.Load()) }

// Write delivers remote input. It never blocks: keys beyond the queue are
// dropped, as is input beyond maxInputBytes per message.
func (s *Session) Write(data []byte) error {
	if len(data) > maxInputBytes {
		data = data[:maxInputBytes]
	}
	s.decMu.Lock()
	keys := s.dec.Feed(data)
	s.decMu.Unlock()
	for _, k := range keys {
		select {
		case s.keys <- k:
		default:
			return nil
		}
	}
	return nil
}

// Resize records the client's window size; full-screen programs redraw.
func (s *Session) Resize(cols, rows uint16) error {
	s.setSize(cols, rows)
	select {
	case s.resized <- struct{}{}:
	default:
	}
	return nil
}

// Done is closed by Close; a fake session never ends on its own.
func (s *Session) Done() <-chan struct{} { return s.done }

// Close stops the program.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		close(s.done)
	})
}

// TTY is what a program sees: decoded keys, the window size and an output
// writer. Programs write "\n"; TTY sends "\r\n", since there is no kernel
// line discipline to translate it.
type TTY struct {
	keys        <-chan term.Key
	resized     <-chan struct{}
	size        func() (int, int)
	emit        func(p []byte, reset bool)
	now         func() time.Time
	atLineStart bool
	lastCR      bool
}

func (t *TTY) translate(p []byte) []byte {
	out := make([]byte, 0, len(p)+8)
	for _, b := range p {
		if b == '\n' && !t.lastCR {
			out = append(out, '\r')
		}
		out = append(out, b)
		t.lastCR = b == '\r'
	}
	if n := len(p); n > 0 {
		last := p[n-1]
		t.atLineStart = last == '\n' || last == '\r'
	}
	return out
}

// Write emits p.
func (t *TTY) Write(p []byte) (int, error) {
	if len(p) > 0 {
		t.emit(t.translate(p), false)
	}
	return len(p), nil
}

// WriteString emits s.
func (t *TTY) WriteString(s string) { t.Write([]byte(s)) }

// Printf formats and emits.
func (t *TTY) Printf(format string, args ...any) { t.WriteString(fmt.Sprintf(format, args...)) }

// Frame emits s as a complete screen: clients attaching later replay only
// this frame and what follows it.
func (t *TTY) Frame(s string) { t.emit(t.translate([]byte(s)), true) }

// ClearScreen clears the screen and homes the cursor.
func (t *TTY) ClearScreen() {
	t.Frame("\x1b[H\x1b[2J")
	t.atLineStart = true
}

// Size returns the window size in columns and rows.
func (t *TTY) Size() (cols, rows int) { return t.size() }

// Width returns the window width.
func (t *TTY) Width() int { c, _ := t.size(); return c }

// AtLineStart reports whether the last output ended a line.
func (t *TTY) AtLineStart() bool { return t.atLineStart }

// Sleep waits d and reports false if Ctrl-C interrupted it. Other keys are
// discarded, like typing ahead into a busy terminal that ignores it.
func (t *TTY) Sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		case k := <-t.keys:
			if k == term.KeyCtrlC {
				t.WriteString("^C\n")
				return false
			}
		}
	}
}
