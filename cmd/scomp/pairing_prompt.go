package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/smart-bit-me/scomp-agent/protocol"
)

// pairPromptTTL matches the relay's pairing lifetime: after it the relay
// rejects the PIN anyway, so the prompt stops waiting.
const pairPromptTTL = 2 * time.Minute

// pairPrompter shows at most one pairing prompt at a time and reads the PIN
// from stdin. A blocked read cannot be cancelled, so one long-lived goroutine
// reads stdin lines and the prompt loop decides what they mean; a line typed
// with no prompt showing is discarded. A prompt ends when a PIN is entered,
// when it expires, or when a newer pair_request replaces it. It is not tied
// to one relay connection: the relay keeps the pairing per agent UID across
// reconnects, so the PIN is submitted on whichever connection is current
// when it is typed, and reported as not submitted when there is none.
type pairPrompter struct {
	in       io.Reader
	out      io.Writer
	ttl      time.Duration
	requests chan string
	start    sync.Once

	mu     sync.Mutex
	submit func(protocol.AgentMsg) bool // current connection; nil when disconnected
	gen    uint64
}

func newPairPrompter(in io.Reader, out io.Writer, ttl time.Duration) *pairPrompter {
	return &pairPrompter{in: in, out: out, ttl: ttl, requests: make(chan string, 4)}
}

// connected installs the current connection's sender and returns a func that
// removes it again (only if no newer connection replaced it meanwhile).
func (p *pairPrompter) connected(submit func(protocol.AgentMsg) bool) (disconnected func()) {
	p.mu.Lock()
	p.gen++
	gen := p.gen
	p.submit = submit
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		if p.gen == gen {
			p.submit = nil
		}
		p.mu.Unlock()
	}
}

// request shows (or replaces) the prompt for pairID. It never blocks the
// relay read loop.
func (p *pairPrompter) request(pairID string) {
	p.start.Do(func() { go p.run() })
	select {
	case p.requests <- pairID:
	default: // a burst of requests: the queued ones are newer than the prompt
	}
}

func (p *pairPrompter) send(msg protocol.AgentMsg) bool {
	p.mu.Lock()
	submit := p.submit
	p.mu.Unlock()
	return submit != nil && submit(msg)
}

func (p *pairPrompter) run() {
	lines := make(chan string)
	go func() {
		defer close(lines)
		reader := bufio.NewReader(p.in)
		for {
			line, err := reader.ReadString('\n')
			if line != "" || err == nil {
				lines <- line
			}
			if err != nil {
				return
			}
		}
	}()

	var (
		pairID  string
		expired <-chan time.Time
		timer   *time.Timer
		noStdin bool
	)
	stop := func() {
		pairID, expired = "", nil
		if timer != nil {
			timer.Stop()
		}
	}
	for {
		select {
		case id := <-p.requests:
			if noStdin {
				fmt.Fprintln(p.out, "\n[scomp] pairing requested, but stdin is not a terminal — run scomp in the foreground to pair.")
				continue
			}
			if pairID != "" {
				fmt.Fprintln(p.out, "\n[scomp] a newer pairing request replaced the previous one.")
			}
			stop()
			pairID = id
			timer = time.NewTimer(p.ttl)
			expired = timer.C
			fmt.Fprint(p.out, "\n[scomp] Pairing requested from a browser.\n"+
				"        Enter the PIN shown there to link this machine: ")

		case line, ok := <-lines:
			if !ok {
				lines, noStdin = nil, true
				if pairID != "" {
					fmt.Fprintln(p.out, "\n[scomp] pairing aborted (stdin not a terminal — run scomp in the foreground).")
				}
				stop()
				continue
			}
			if pairID == "" {
				continue // typed while no prompt was showing
			}
			id := pairID
			stop()
			pin := strings.TrimSpace(line)
			switch {
			case pin == "":
				fmt.Fprintln(p.out, "[scomp] pairing aborted (empty PIN).")
			case p.send(protocol.AgentMsg{Type: "pair_submit", PairID: id, PIN: pin}):
				fmt.Fprintln(p.out, "[scomp] PIN submitted — check your browser.")
			default:
				fmt.Fprintln(p.out, "[scomp] PIN not submitted: not connected to the relay. Start pairing again from the browser.")
			}

		case <-expired:
			stop()
			fmt.Fprintln(p.out, "\n[scomp] pairing request expired. Start pairing again from the browser.")
		}
	}
}
