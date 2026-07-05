// scomp is the agent that runs on the developer's machine.
// It manages PTY sessions locally, connects outbound to scomp-server,
// and relays PTY traffic between local processes and remote browsers.
package main

import (
	"context"
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
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/smart-bit-me/scomp-agent/internal/auth"
	"github.com/smart-bit-me/scomp-agent/internal/screensess"
	"github.com/smart-bit-me/scomp-agent/internal/sessions"
	"github.com/smart-bit-me/scomp-agent/internal/tmuxsess"
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

func main() {
	serverURL := flag.String("server", "wss://link.scomp.me/agent", "scomp-server WebSocket URL")
	modeFlag := flag.String("mode", "full", "session mode: full | readonly | approved-only")
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

	var defaultMode sessions.SessionMode
	switch *modeFlag {
	case "readonly":
		defaultMode = sessions.ModeReadonly
	case "approved-only":
		defaultMode = sessions.ModeApproved
	default:
		defaultMode = sessions.ModeFull
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

	newSess := make(chan lazySession, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchSessions(ctx, reg, defaultMode, onTmuxActivated, newSess)

	for {
		lazy := discoverSessions(reg, defaultMode, onTmuxActivated)
		if err := runAgent(*serverURL, uid, reg, lazy, newSess); err != nil {
			log.Printf("server disconnected (%v), retrying in 5s…", err)
		}
		time.Sleep(5 * time.Second)
	}
}

// discoverSessions builds lazy descriptors for detected tmux and screen sessions.
// onTmuxActivated is called with the tmux session name when a linked session is created.
func discoverSessions(reg *sessions.Registry, mode sessions.SessionMode, onTmuxActivated func(string)) (lazy []lazySession) {
	for _, s := range tmuxsess.List() {
		id := newSessionID()
		name := s.Name
		lazy = append(lazy, lazySession{
			id:      id,
			cmd:     "tmux:" + name,
			created: time.Now(),
			mode:    mode,
			activate: func(cols, rows uint16) error {
				cmd, args := tmuxsess.LinkedSessionArgs(name)
				_, _, err := reg.CreateWithID(id, cmd, args, cols, rows)
				if err == nil {
					onTmuxActivated(name)
				}
				return err
			},
		})
		log.Printf("discovered tmux: %s (attaches on first browser connect)", name)
	}

	for _, s := range screensess.List() {
		id := newSessionID()
		fullName := s.FullName
		shortName := s.Name
		lazy = append(lazy, lazySession{
			id:      id,
			cmd:     "screen:" + shortName,
			created: time.Now(),
			mode:    mode,
			activate: func(cols, rows uint16) error {
				cmd, args := screensess.AttachArgs(fullName)
				_, _, err := reg.CreateWithID(id, cmd, args, cols, rows)
				return err
			},
		})
		log.Printf("discovered screen: %s (attaches on first browser connect)", shortName)
	}

	if len(lazy) == 0 {
		log.Printf("no tmux or screen sessions found — start one and the agent will detect it automatically")
	}
	return
}

// watchSessions watches tmux and screen socket directories for new sessions using
// filesystem events (inotify on Linux). New sessions are sent on newSess without polling.
//
// Limitation: detects a new tmux *server* (socket file creation) but not new sessions
// within an already-running tmux server — those all share one socket file.
func watchSessions(
	ctx context.Context,
	reg *sessions.Registry,
	mode sessions.SessionMode,
	onTmuxActivated func(string),
	newSess chan<- lazySession,
) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("sesswatch: %v", err)
		return
	}
	defer w.Close()

	known := make(map[string]bool)

	addDir := func(dir string) {
		if dir == "" {
			return
		}
		if err := w.Add(dir); err != nil {
			if !os.IsNotExist(err) {
				log.Printf("sesswatch: watch %s: %v", dir, err)
			}
		}
	}
	addDir(tmuxsess.SocketDir())
	addDir(screensess.SocketDir())

	scan := func() {
		for _, s := range tmuxsess.List() {
			key := "tmux:" + s.Name
			if known[key] {
				continue
			}
			known[key] = true
			name := s.Name
			id := newSessionID()
			ls := lazySession{
				id:      id,
				cmd:     key,
				created: time.Now(),
				mode:    mode,
				activate: func(cols, rows uint16) error {
					cmd, args := tmuxsess.LinkedSessionArgs(name)
					_, _, err := reg.CreateWithID(id, cmd, args, cols, rows)
					if err == nil {
						onTmuxActivated(name)
					}
					return err
				},
			}
			log.Printf("sesswatch: new tmux session: %s", name)
			select {
			case newSess <- ls:
			case <-ctx.Done():
				return
			}
		}
		for _, s := range screensess.List() {
			key := "screen:" + s.Name
			if known[key] {
				continue
			}
			known[key] = true
			fullName := s.FullName
			shortName := s.Name
			id := newSessionID()
			ls := lazySession{
				id:      id,
				cmd:     key,
				created: time.Now(),
				mode:    mode,
				activate: func(cols, rows uint16) error {
					cmd, args := screensess.AttachArgs(fullName)
					_, _, err := reg.CreateWithID(id, cmd, args, cols, rows)
					return err
				},
			}
			log.Printf("sesswatch: new screen session: %s", shortName)
			select {
			case newSess <- ls:
			case <-ctx.Done():
				return
			}
		}
	}

	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if ev.Has(fsnotify.Create) {
				// If a new directory appeared (e.g. tmux socket dir just created),
				// watch it so we catch socket files created inside it.
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					w.Add(ev.Name)
				}
				scan()
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("sesswatch: %v", err)
		case <-ctx.Done():
			return
		}
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

func runAgent(serverURL, uid string, reg *sessions.Registry, lazy []lazySession, newSess <-chan lazySession) error {
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
	})
	if err := conn.WriteMessage(websocket.TextMessage, hello); err != nil {
		return err
	}

	sendCh := make(chan []byte, 512)
	connDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Exit on connDone rather than on a close(sendCh): other goroutines
		// (forwarders, the main loop) may still call send() during teardown,
		// and closing sendCh under them would panic with "send on closed channel".
		for {
			select {
			case <-connDone:
				return
			case data := <-sendCh:
				conn.WriteMessage(websocket.TextMessage, data)
			}
		}
	}()

	send := func(msg protocol.AgentMsg) {
		data, _ := json.Marshal(msg)
		select {
		case sendCh <- data:
		case <-connDone:
		default:
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

	startForwarder := func(id string) {
		sess, ok := reg.Get(id)
		if !ok {
			return
		}
		outputCh := make(chan []byte, 256)
		sess.Subscribe((chan<- []byte)(outputCh))

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
	announced := make(map[string]bool, len(lazy))
	for i := range lazy {
		ls := &lazy[i]
		sendSessionAdd(ls.id, ls.cmd, ls.created, ls.mode, false, 0, 0)
		lazyMap[ls.id] = ls
		announced[ls.id] = true
	}

	// Read incoming server messages in a goroutine so we can also select on newSess.
	readCh := make(chan protocol.ServerMsg, 64)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(readCh)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				close(connDone)
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
		case msg, ok := <-readCh:
			if !ok {
				wg.Wait()
				return fmt.Errorf("connection closed")
			}
			handleServerMsg(msg, reg, modes, lazyMap, send, sendSessionAdd, startForwarder)

		case ls := <-newSess:
			if announced[ls.id] {
				continue
			}
			announced[ls.id] = true
			lsCopy := ls
			lazyMap[ls.id] = &lsCopy
			sendSessionAdd(ls.id, ls.cmd, ls.created, ls.mode, false, 0, 0)
		}
	}
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
		sess.Resize(msg.Cols, msg.Rows)

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
	}
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
