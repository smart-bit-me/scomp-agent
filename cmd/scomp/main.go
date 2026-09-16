// scomp is the agent that runs on the developer's machine.
// It manages PTY sessions locally, connects outbound to scomp-server,
// and relays PTY traffic between local processes and remote browsers.
package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/smart-bit-me/scomp-agent/internal/auth"
	"github.com/smart-bit-me/scomp-agent/internal/sessions"
	"github.com/smart-bit-me/scomp-agent/protocol"
)

// lazySession is a discovered multiplexer session not yet attached.
// The PTY is created on-demand the first time a browser client connects,
// preventing any disruption to the existing terminal session at startup.
type lazySession struct {
	id       string
	cmd      string // display label shown in the browser (e.g. "tmux:mysession")
	created  time.Time
	mode     sessions.SessionMode
	activate func(cols, rows uint16) error
}

var version = "dev" // overridden by -ldflags at release build time

const (
	agentPingInterval = 30 * time.Second
	agentWriteTimeout = 10 * time.Second
)

func main() {
	serverURL := flag.String("server", "wss://link.scomp.me/agent", "scomp-server WebSocket URL")
	modeFlag := flag.String("mode", "readonly", "session mode: full | readonly | approved-only")
	noQR := flag.Bool("no-qr", false, "skip QR code, print URL only")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("scomp %s\n", version)
		os.Exit(0)
	}

	log.Printf("scomp %s starting", version)

	configDir := filepath.Join(os.Getenv("HOME"), ".config", "scomp")
	uid, err := auth.LoadOrCreateUID(configDir)
	if err != nil {
		log.Fatalf("uid: %v", err)
	}
	key, err := auth.LoadOrCreateKey(configDir)
	if err != nil {
		log.Fatalf("key: %v", err)
	}

	defaultMode, err := parseSessionMode(*modeFlag)
	if err != nil {
		log.Fatal(err)
	}

	reg := sessions.New()

	var (
		activatedTmuxMu sync.Mutex
		activatedTmux   []string
	)
	onTmuxActivated := func(name string) {
		activatedTmuxMu.Lock()
		activatedTmux = append(activatedTmux, name)
		activatedTmuxMu.Unlock()
	}
	cleanupTmux := func() {
		activatedTmuxMu.Lock()
		names := append([]string(nil), activatedTmux...)
		activatedTmuxMu.Unlock()
		for _, name := range names {
			linked := "scomp-" + name
			if err := exec.Command("tmux", "kill-session", "-t", linked).Run(); err != nil {
				log.Printf("kill tmux session %s: %v", linked, err)
			} else {
				log.Printf("killed tmux session %s", linked)
			}
		}
	}
	defer cleanupTmux()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cleanupTmux()
		os.Exit(0)
	}()

	mobileURL := serverBaseURL(*serverURL) + "/?uid=" + uid
	printBanner(uid, *serverURL, mobileURL, *noQR)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	catalog := newSessionCatalog(reg, defaultMode, onTmuxActivated)
	go watchSessions(ctx, catalog)

	for {
		if err := runAgent(*serverURL, uid, key, reg, catalog); err != nil {
			log.Printf("server disconnected (%v), retrying in 5s…", err)
		}
		time.Sleep(5 * time.Second)
	}
}

func parseSessionMode(value string) (sessions.SessionMode, error) {
	switch value {
	case "full":
		return sessions.ModeFull, nil
	case "readonly":
		return sessions.ModeReadonly, nil
	case "approved-only":
		return sessions.ModeApproved, nil
	default:
		return "", fmt.Errorf("invalid --mode %q (expected full, readonly, or approved-only)", value)
	}
}

func printBanner(uid, serverURL, mobileURL string, noQR bool) {
	fmt.Println()
	fmt.Println("  Scomp Agent")
	fmt.Printf("  UID:    %s\n", uid)
	fmt.Printf("  Server: %s\n", serverURL)
	fmt.Printf("  URL:    %s\n", mobileURL)
	fmt.Println()

	if !noQR {
		qr, err := qrcode.New(mobileURL, qrcode.Medium)
		if err == nil {
			fmt.Println(qr.ToSmallString(false))
		}
	}

	fmt.Println("  Scan the QR code with your phone or open the URL.")
	fmt.Println()
}

func wsToHTTP(wsURL string) string {
	wsURL = strings.TrimRight(wsURL, "/")
	if strings.HasPrefix(wsURL, "wss://") {
		return "https://" + wsURL[6:]
	}
	if strings.HasPrefix(wsURL, "ws://") {
		return "http://" + wsURL[5:]
	}
	return wsURL
}

// serverBaseURL returns the HTTP root of the relay server (scheme+host only,
// no path) so the mobile QR URL points to the browser UI, not the /agent endpoint.
func serverBaseURL(wsURL string) string {
	u, err := url.Parse(wsURL)
	if err != nil {
		return wsToHTTP(wsURL)
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	default:
		u.Scheme = "http"
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

// answerChallenge reads the server's "challenge" message, signs its nonce with
// the agent's private key, and replies with an "auth" message. The read is
// deadline-bounded so a silent relay cannot wedge the connect.
func answerChallenge(conn *websocket.Conn, key ed25519.PrivateKey) error {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	var msg protocol.ServerMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return err
	}
	if msg.Type != "challenge" || msg.Nonce == "" {
		return fmt.Errorf("expected challenge, got %q", msg.Type)
	}
	nonce, err := hex.DecodeString(msg.Nonce)
	if err != nil {
		return fmt.Errorf("bad challenge nonce: %w", err)
	}
	resp, _ := json.Marshal(protocol.AgentMsg{
		Type: "auth",
		Sig:  hex.EncodeToString(ed25519.Sign(key, nonce)),
	})
	return conn.WriteMessage(websocket.TextMessage, resp)
}

func runAgent(serverURL, uid string, key ed25519.PrivateKey, reg *sessions.Registry, catalog *sessionCatalog) error {
	conn, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Cap inbound frame size: the relay is the sole (trusted-but-remote) peer,
	// and an unbounded ReadMessage would let a compromised relay OOM the agent.
	conn.SetReadLimit(1 << 20) // 1 MiB

	hostname, _ := os.Hostname()
	hello, _ := json.Marshal(protocol.AgentMsg{
		Type:     "hello",
		UID:      uid,
		Version:  "1.0",
		Hostname: hostname,
		PubKey:   auth.PublicKeyHex(key),
	})
	if err := conn.WriteMessage(websocket.TextMessage, hello); err != nil {
		return err
	}

	// Proof-of-possession: the server replies with a challenge nonce we must sign
	// with our private key (F3). This runs before the main loop so a rogue agent
	// that only knows the uid cannot get past the handshake.
	if err := answerChallenge(conn, key); err != nil {
		return err
	}
	// Reconcile against the actual tmux/Screen sockets after every relay
	// reconnect. A long-running filesystem watcher can miss an event (for
	// example after an overflow); without this refresh, a relay restart would
	// receive that stale catalog until another socket event such as screen -rd.
	catalog.scan(false)
	lazy, sessionChanges, unsubscribe := catalog.snapshotAndSubscribe()
	defer unsubscribe()

	sendCh := make(chan []byte, 512)
	connDone := make(chan struct{})
	connErr := make(chan error, 1)
	var closeConnOnce sync.Once
	closeConnection := func(err error) {
		closeConnOnce.Do(func() {
			if err != nil {
				connErr <- err
			}
			close(connDone)
			// Unblock ReadMessage immediately when the writer discovers a dead
			// connection. Without this, reconnect waits for the read side or the
			// network stack to notice the failure independently.
			conn.Close()
		})
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ping := time.NewTicker(agentPingInterval)
		defer ping.Stop()
		// Exit on connDone rather than on a close(sendCh): other goroutines
		// (forwarders, the main loop) may still call send() during teardown,
		// and closing sendCh under them would panic with "send on closed channel".
		for {
			select {
			case <-connDone:
				return
			case data := <-sendCh:
				conn.SetWriteDeadline(time.Now().Add(agentWriteTimeout))
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					closeConnection(fmt.Errorf("write: %w", err))
					return
				}
			case <-ping.C:
				// Keep the otherwise-idle agent connection alive through reverse
				// proxies and load balancers. Gorilla handles the peer's pong in
				// ReadMessage; all writes stay in this one goroutine.
				conn.SetWriteDeadline(time.Now().Add(agentWriteTimeout))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					closeConnection(fmt.Errorf("ping: %w", err))
					return
				}
			}
		}
	}()

	send := func(msg protocol.AgentMsg) {
		data, _ := json.Marshal(msg)
		select {
		case sendCh <- data:
		case <-connDone:
		default:
			// Dropping terminal input/control/output silently corrupts the remote
			// view. Reconnect instead; the next attach receives a fresh snapshot.
			closeConnection(fmt.Errorf("outbound queue full"))
		}
	}

	sendSessionAdd := func(id, cmd string, created time.Time, mode sessions.SessionMode, active bool, cols, rows uint16) {
		send(protocol.AgentMsg{
			Type:      "session_add",
			SessionID: id,
			Info: &protocol.SessionInfo{
				ID:      id,
				Cmd:     cmd,
				Created: created,
				Mode:    protocol.SessionMode(mode),
				Active:  active,
				Cols:    cols,
				Rows:    rows,
			},
		})
	}

	modes := make(map[string]sessions.SessionMode)
	sessionEnded := make(chan string, 64)

	startForwarder := func(id string) {
		sess, ok := reg.Get(id)
		if !ok {
			return
		}
		outputCh := make(chan []byte, 256)
		sess.Subscribe((chan<- []byte)(outputCh), func() {
			closeConnection(fmt.Errorf("PTY output subscriber too slow"))
		})

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer sess.Unsubscribe((chan<- []byte)(outputCh))
			for {
				select {
				case <-connDone:
					return
				case <-sess.Done():
					send(protocol.AgentMsg{Type: "session_remove", SessionID: id})
					select {
					case sessionEnded <- id:
					case <-connDone:
					}
					return
				case data := <-outputCh:
					send(protocol.AgentMsg{
						Type:      "output",
						SessionID: id,
						Data:      base64.StdEncoding.EncodeToString(data),
					})
				}
			}
		}()
	}

	lazyMap := make(map[string]*lazySession, len(lazy))
	availableLazy := make(map[string]lazySession, len(lazy))
	for i := range lazy {
		ls := &lazy[i]
		availableLazy[ls.id] = *ls
		if _, active := reg.Get(ls.id); active {
			modes[ls.id] = ls.mode
			startForwarder(ls.id)
			sendSessionAdd(ls.id, ls.cmd, ls.created, ls.mode, true, 0, 0)
		} else {
			sendSessionAdd(ls.id, ls.cmd, ls.created, ls.mode, false, 0, 0)
			lazyMap[ls.id] = ls
		}
	}

	// Read incoming server messages in a goroutine so we can also select on
	// event-driven multiplexer lifecycle changes.
	readCh := make(chan protocol.ServerMsg, 64)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(readCh)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				closeConnection(fmt.Errorf("read: %w", err))
				return
			}
			var msg protocol.ServerMsg
			if err := json.Unmarshal(raw, &msg); err == nil {
				readCh <- msg
			}
		}
	}()

	for {
		select {
		case err := <-connErr:
			wg.Wait()
			return err

		case msg, ok := <-readCh:
			if !ok {
				wg.Wait()
				select {
				case err := <-connErr:
					return err
				default:
					return fmt.Errorf("connection closed")
				}
			}
			handleServerMsg(msg, reg, modes, lazyMap, send, sendSessionAdd, startForwarder)

		case change := <-sessionChanges:
			if change.removeID != "" {
				delete(lazyMap, change.removeID)
				delete(availableLazy, change.removeID)
				delete(modes, change.removeID)
				send(protocol.AgentMsg{Type: "session_remove", SessionID: change.removeID})
				continue
			}
			if change.add == nil {
				continue
			}
			ls := *change.add
			availableLazy[ls.id] = ls
			lazyMap[ls.id] = &ls
			sendSessionAdd(ls.id, ls.cmd, ls.created, ls.mode, false, 0, 0)

		case id := <-sessionEnded:
			delete(modes, id)
			ls, ok := restoreLazySession(id, availableLazy, lazyMap)
			if !ok {
				continue
			}
			// `screen -rd` terminates the agent's active `screen -x` display but
			// not the underlying Screen session. Re-advertise the stable catalog
			// entry as inactive so it remains attachable from Android/web.
			sendSessionAdd(ls.id, ls.cmd, ls.created, ls.mode, false, 0, 0)
		}
	}
}

func restoreLazySession(
	id string,
	available map[string]lazySession,
	lazyMap map[string]*lazySession,
) (lazySession, bool) {
	ls, ok := available[id]
	if !ok {
		return lazySession{}, false
	}
	entry := ls
	lazyMap[id] = &entry
	return ls, true
}

func handleServerMsg(
	msg protocol.ServerMsg,
	reg *sessions.Registry,
	modes map[string]sessions.SessionMode,
	lazyMap map[string]*lazySession,
	send func(protocol.AgentMsg),
	sendSessionAdd func(id, cmd string, created time.Time, mode sessions.SessionMode, active bool, cols, rows uint16),
	startForwarder func(id string),
) {
	switch msg.Type {
	case "input":
		sess, ok := reg.Get(msg.SessionID)
		if !ok {
			return
		}
		if modes[msg.SessionID] == sessions.ModeReadonly {
			return
		}
		data, err := base64.StdEncoding.DecodeString(msg.Data)
		if err != nil {
			return
		}
		if modes[msg.SessionID] == sessions.ModeApproved && !isApproved(data) {
			return
		}
		sess.Write(data)

	case "resize":
		sess, ok := reg.Get(msg.SessionID)
		if !ok {
			return
		}
		if err := sess.Resize(msg.Cols, msg.Rows); err != nil {
			log.Printf("resize %s to %dx%d: %v", msg.SessionID, msg.Cols, msg.Rows, err)
		}

	case "client_attach":
		if ls, ok := lazyMap[msg.SessionID]; ok {
			if _, already := reg.Get(ls.id); !already {
				cols, rows := msg.Cols, msg.Rows
				if cols == 0 {
					cols = 80
				}
				if rows == 0 {
					rows = 24
				}
				if err := ls.activate(cols, rows); err != nil {
					log.Printf("activate %s (%s): %v", ls.cmd, ls.id, err)
					return
				}
				modes[ls.id] = ls.mode
				startForwarder(ls.id)
				// Notify the server (and browser) that the session is now live.
				sendSessionAdd(ls.id, ls.cmd, ls.created, ls.mode, true, cols, rows)
				log.Printf("activated: %s (%s)", ls.cmd, ls.id)
			}
			delete(lazyMap, ls.id)
		}
		snap := reg.Snapshot(msg.SessionID)
		send(protocol.AgentMsg{
			Type:      "snapshot",
			SessionID: msg.SessionID,
			ClientID:  msg.ClientID,
			Data:      base64.StdEncoding.EncodeToString(snap),
		})

	case "pair_request":
		// A signed-in browser asked to link this host. Prompt on the terminal;
		// the user must type the PIN shown in their browser. This proves TTY
		// access to the host, not just knowledge of the uid (F1). Runs in a
		// goroutine so the blocking stdin read does not stall the read loop.
		go promptPairPIN(msg.PairID, send)
	}
}

// pairInProgress ensures only one terminal PIN prompt is active at a time, even
// if the server (or a rogue relay) sends overlapping pair_request messages.
var pairInProgress atomic.Bool

// promptPairPIN prints a pairing prompt on the agent's terminal, reads one line
// from stdin, and replies with pair_submit carrying the typed PIN. When stdin is
// not a terminal (agent backgrounded / piped) the read returns EOF with no data
// and pairing is aborted — pairing requires running scomp in the foreground.
func promptPairPIN(pairID string, send func(protocol.AgentMsg)) {
	if !pairInProgress.CompareAndSwap(false, true) {
		return
	}
	defer pairInProgress.Store(false)

	fmt.Print("\n[scomp] Pairing requested from a browser.\n" +
		"        Enter the PIN shown there to link this machine: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	pin := strings.TrimSpace(line)
	if pin == "" {
		if err != nil {
			fmt.Println("\n[scomp] pairing aborted (stdin not a terminal — run scomp in the foreground).")
		} else {
			fmt.Println("[scomp] pairing aborted (empty PIN).")
		}
		return
	}
	send(protocol.AgentMsg{Type: "pair_submit", PairID: pairID, PIN: pin})
	fmt.Println("[scomp] PIN submitted — check your browser.")
}

func newSessionID() string {
	// 16 bytes = 128 bits: avoids birthday collisions that a 32-bit ID would
	// hit around a few thousand sessions (a collision misroutes input/attach).
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("generating session id: %v", err)
	}
	return hex.EncodeToString(b)
}

var approvedSequences = [][]byte{
	[]byte("y\r"), []byte("y\n"),
	[]byte("n\r"), []byte("n\n"),
	{0x03}, // Ctrl+C
	{0x0d}, // Enter
	{0x09}, // Tab
	{0x1b}, // Esc
}

func isApproved(data []byte) bool {
	for _, seq := range approvedSequences {
		if len(data) == len(seq) {
			match := true
			for i := range seq {
				if data[i] != seq[i] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}
