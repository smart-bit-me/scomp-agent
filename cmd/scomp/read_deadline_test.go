package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/smart-bit-me/scomp-agent/protocol"
)

// startRelay runs a WebSocket server whose per-connection behavior is serve.
func startRelay(t *testing.T, serve func(*websocket.Conn)) string {
	t.Helper()
	upgrader := websocket.Upgrader{}
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		serve(conn)
		<-done // stay silent but keep the TCP connection open (half-open relay)
	}))
	t.Cleanup(func() {
		close(done)
		srv.Close()
	})
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// runReader dials url, arms the rolling read deadline, and runs the agent's
// reader loop, returning the error it reports and the time it took.
func runReader(t *testing.T, url string, timeout time.Duration) (time.Duration, error) {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	readCh := make(chan protocol.ServerMsg, 64)
	connDone := make(chan struct{})
	errCh := make(chan error, 1)
	start := time.Now()
	go readServerMessages(newDeadlineReader(conn, timeout), readCh, connDone, func(err error) {
		errCh <- err
		close(connDone)
	})
	go func() {
		for range readCh {
		}
	}()
	select {
	case err := <-errCh:
		return time.Since(start), err
	case <-time.After(10 * time.Second):
		t.Fatal("reader did not give up on a silent relay")
		return 0, nil
	}
}

func isTimeout(err error) bool {
	return err != nil && strings.Contains(err.Error(), "timeout")
}

func TestReadDeadlineClosesSilentRelay(t *testing.T) {
	url := startRelay(t, func(conn *websocket.Conn) {
		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"noop"}`))
	})
	elapsed, err := runReader(t, url, 200*time.Millisecond)
	if !isTimeout(err) {
		t.Fatalf("reader error = %v, want timeout", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("reader took %v to detect silent relay", elapsed)
	}
}

func TestReadDeadlineExtendedByPingAndAnswered(t *testing.T) {
	const timeout = 200 * time.Millisecond
	const pingFor = 800 * time.Millisecond
	var pongs atomic.Int32
	url := startRelay(t, func(conn *websocket.Conn) {
		conn.SetPongHandler(func(string) error {
			pongs.Add(1)
			return nil
		})
		go func() { // process control frames (pongs) from the agent
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
		for end := time.Now().Add(pingFor); time.Now().Before(end); {
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
				return
			}
			time.Sleep(timeout / 4)
		}
		// Then go silent without closing.
	})
	elapsed, err := runReader(t, url, timeout)
	if !isTimeout(err) {
		t.Fatalf("reader error = %v, want timeout", err)
	}
	if elapsed < pingFor {
		t.Fatalf("reader timed out after %v while relay was still pinging", elapsed)
	}
	if pongs.Load() == 0 {
		t.Fatal("agent did not answer relay pings with pongs")
	}
}

func TestReadDeadlineExtendedByPong(t *testing.T) {
	const timeout = 200 * time.Millisecond
	const pongFor = 800 * time.Millisecond
	url := startRelay(t, func(conn *websocket.Conn) {
		for end := time.Now().Add(pongFor); time.Now().Before(end); {
			if err := conn.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second)); err != nil {
				return
			}
			time.Sleep(timeout / 4)
		}
	})
	elapsed, err := runReader(t, url, timeout)
	if !isTimeout(err) {
		t.Fatalf("reader error = %v, want timeout", err)
	}
	if elapsed < pongFor {
		t.Fatalf("reader timed out after %v while relay was still sending pongs", elapsed)
	}
}

func TestReadDeadlineExtendedByMessages(t *testing.T) {
	const timeout = 200 * time.Millisecond
	const sendFor = 800 * time.Millisecond
	url := startRelay(t, func(conn *websocket.Conn) {
		for end := time.Now().Add(sendFor); time.Now().Before(end); {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"noop"}`)); err != nil {
				return
			}
			time.Sleep(timeout / 4)
		}
	})
	elapsed, err := runReader(t, url, timeout)
	if !isTimeout(err) {
		t.Fatalf("reader error = %v, want timeout", err)
	}
	if elapsed < sendFor {
		t.Fatalf("reader timed out after %v while relay was still sending", elapsed)
	}
}
