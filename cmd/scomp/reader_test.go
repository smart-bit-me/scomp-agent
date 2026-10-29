package main

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/smart-bit-me/scomp-agent/protocol"
)

// floodReader returns a valid server message on every call, like a relay that
// keeps sending while the agent tears the connection down.
type floodReader struct{}

func (floodReader) ReadMessage() (int, []byte, error) {
	return websocket.TextMessage, []byte(`{"type":"input","session_id":"s"}`), nil
}

func TestReadServerMessagesStopsOnConnDoneWithFullChannel(t *testing.T) {
	readCh := make(chan protocol.ServerMsg, 64)
	connDone := make(chan struct{})
	returned := make(chan struct{})
	go func() {
		readServerMessages(floodReader{}, readCh, connDone, func(error) {})
		close(returned)
	}()

	// Nobody drains readCh: wait until it is full, as when the main loop has
	// switched to wg.Wait() after connErr.
	deadline := time.Now().Add(5 * time.Second)
	for len(readCh) < cap(readCh) {
		if time.Now().After(deadline) {
			t.Fatal("readCh never filled")
		}
		time.Sleep(time.Millisecond)
	}
	close(connDone)

	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("reader blocked on a full readCh after connDone closed")
	}
	// readCh must be closed after the buffered messages.
	for range readCh {
	}
}
