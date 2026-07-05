package pty

import (
	"os"
	"os/exec"
	"sync"

	cpty "github.com/creack/pty"

	"github.com/smart-bit-me/scomp-agent/internal/ring"
)

// modeState is the parser state for tracking DEC private mode sequences.
type modeState int

const (
	stateNormal modeState = iota
	stateEsc
	stateCsi
	stateCsiQ  // saw ESC [ ?
	stateCsiQN // accumulating digits after ESC [ ?
)

// modeTracker is a byte-level state machine that tracks DECCKM (application
// cursor keys mode, CSI ? 1 h/l) so we can transform arrow-key input correctly.
type modeTracker struct {
	state  modeState
	param  []byte
	DECCKM bool
}

func (t *modeTracker) process(p []byte) {
	for _, b := range p {
		switch t.state {
		case stateNormal:
			if b == 0x1b {
				t.state = stateEsc
			}
		case stateEsc:
			if b == '[' {
				t.state = stateCsi
			} else {
				t.state = stateNormal
			}
		case stateCsi:
			if b == '?' {
				t.state = stateCsiQ
				t.param = t.param[:0]
			} else {
				t.state = stateNormal
			}
		case stateCsiQ:
			if b >= '0' && b <= '9' {
				t.param = append(t.param, b)
				t.state = stateCsiQN
			} else {
				t.state = stateNormal
			}
		case stateCsiQN:
			switch {
			case (b >= '0' && b <= '9') || b == ';':
				t.param = append(t.param, b)
			case b == 'h' || b == 'l':
				if string(t.param) == "1" {
					t.DECCKM = b == 'h'
				}
				t.param = t.param[:0]
				t.state = stateNormal
			default:
				t.param = t.param[:0]
				t.state = stateNormal
			}
		}
	}
}

// transformInput applies DECCKM arrow-key transformation when active.
// The browser sends normal-mode sequences (ESC [ A/B/C/D); in application
// cursor-keys mode the terminal expects ESC O A/B/C/D instead.
func (t *modeTracker) transformInput(data []byte) []byte {
	if !t.DECCKM || len(data) != 3 || data[0] != 0x1b || data[1] != '[' {
		return data
	}
	switch data[2] {
	case 'A', 'B', 'C', 'D':
		return []byte{0x1b, 'O', data[2]}
	}
	return data
}

// Session wraps a PTY-backed process and fans out its output to subscribers.
type Session struct {
	Cmd string

	ptmx *os.File
	cmd  *exec.Cmd
	mu   sync.Mutex // guards Resize

	ring      *ring.Buffer
	tracker   modeTracker
	trackerMu sync.RWMutex // guards tracker

	subsMu sync.RWMutex
	subs   map[chan<- []byte]struct{}

	readOnce  sync.Once
	closeOnce sync.Once
	done      chan struct{}
}

func New(shellCmd string, args []string, cols, rows uint16) (*Session, error) {
	cmd := exec.Command(shellCmd, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := cpty.StartWithSize(cmd, &cpty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, err
	}

	s := &Session{
		Cmd:  shellCmd,
		ptmx: ptmx,
		cmd:  cmd,
		ring: ring.New(ring.DefaultSize),
		subs: make(map[chan<- []byte]struct{}),
		done: make(chan struct{}),
	}
	s.startReadLoop()
	return s, nil
}

func (s *Session) startReadLoop() {
	s.readOnce.Do(func() { go s.readLoop() })
}

func (s *Session) readLoop() {
	defer s.closeOnce.Do(func() { close(s.done) })
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			s.ring.Write(data)
			s.trackerMu.Lock()
			s.tracker.process(data)
			s.trackerMu.Unlock()
			s.broadcast(data)
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) broadcast(data []byte) {
	s.subsMu.RLock()
	defer s.subsMu.RUnlock()
	for ch := range s.subs {
		select {
		case ch <- data:
		default:
		}
	}
}

func (s *Session) Subscribe(ch chan<- []byte) {
	s.subsMu.Lock()
	s.subs[ch] = struct{}{}
	s.subsMu.Unlock()
}

func (s *Session) Unsubscribe(ch chan<- []byte) {
	s.subsMu.Lock()
	delete(s.subs, ch)
	s.subsMu.Unlock()
}

// Write sends data to the PTY, applying DECCKM arrow-key transformation.
func (s *Session) Write(data []byte) error {
	s.trackerMu.RLock()
	transformed := s.tracker.transformInput(data)
	s.trackerMu.RUnlock()
	_, err := s.ptmx.Write(transformed)
	return err
}

// Resize resizes the PTY window.
func (s *Session) Resize(cols, rows uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cpty.Setsize(s.ptmx, &cpty.Winsize{Rows: rows, Cols: cols})
}

// Snapshot returns the ring buffer contents for replay on reconnect.
func (s *Session) Snapshot() []byte {
	return s.ring.Snapshot()
}

// DECCKMActive reports whether application cursor key mode is currently active.
func (s *Session) DECCKMActive() bool {
	s.trackerMu.RLock()
	defer s.trackerMu.RUnlock()
	return s.tracker.DECCKM
}

// Done returns a channel that is closed when the PTY process exits.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Close terminates the session.
func (s *Session) Close() {
	s.ptmx.Close()
	s.closeOnce.Do(func() { close(s.done) })
	if s.cmd != nil {
		go func() {
			s.cmd.Process.Kill()
			s.cmd.Wait()
		}()
	}
}
