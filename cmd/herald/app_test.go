package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bastian110/herald/internal/protocol"
)

// mockTelegram simulates the Telegram Bot API: one inbound update on the first
// getUpdates call, and records any sendMessage calls.
type mockTelegram struct {
	mu            sync.Mutex
	updatesServed bool
	sentChatID    int64
	sentText      string
	sentCount     int
	ready         chan struct{} // closed by the test once a harness has subscribed
}

func (m *mockTelegram) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			m.mu.Lock()
			first := !m.updatesServed
			m.updatesServed = true
			m.mu.Unlock()
			if first {
				// Hold the first update until the test signals a harness is
				// connected, so the broadcast isn't dropped (no persistence).
				<-m.ready
				json.NewEncoder(w).Encode(map[string]interface{}{
					"ok": true,
					"result": []map[string]interface{}{
						{
							"update_id": 1,
							"message": map[string]interface{}{
								"message_id": 1,
								"text":       "ping from user",
								"chat":       map[string]interface{}{"id": float64(777)},
								"from":       map[string]interface{}{"username": "bastian"},
							},
						},
					},
				})
			} else {
				// long-poll: brief block then empty
				time.Sleep(20 * time.Millisecond)
				json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "result": []interface{}{}})
			}
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			var body struct {
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			m.mu.Lock()
			m.sentChatID = body.ChatID
			m.sentText = body.Text
			m.sentCount++
			m.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}
}

func TestEndToEndRoundtrip(t *testing.T) {
	mock := &mockTelegram{ready: make(chan struct{})}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()

	socketPath := filepath.Join(t.TempDir(), "herald-e2e.sock")

	cfg := Config{
		Token:         "test-token",
		BaseURL:       srv.URL,
		SocketPath:    socketPath,
		PollTimeout:   1,
		DefaultChatID: 555,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	go run(ctx, cfg) //nolint:errcheck

	// Wait for the socket to come up.
	conn := dialWithRetry(t, socketPath, 2*time.Second)
	defer conn.Close()

	scanner := bufio.NewScanner(conn)

	// Give the connection handler a moment to subscribe to the router, then
	// release the mock's first getUpdates so the broadcast has a listener.
	time.Sleep(100 * time.Millisecond)
	close(mock.ready)

	// 1. Inbound: the mock's getUpdates message should be broadcast to us.
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if !scanner.Scan() {
		t.Fatal("did not receive inbound broadcast from mock Telegram")
	}
	var inbound protocol.Envelope
	if err := json.Unmarshal(scanner.Bytes(), &inbound); err != nil {
		t.Fatalf("decode inbound: %v", err)
	}
	if inbound.Op != "message" || inbound.Text != "ping from user" {
		t.Errorf("inbound: want op=message text=\"ping from user\", got %+v", inbound)
	}
	if inbound.From != "bastian" || inbound.ChatID != 777 {
		t.Errorf("inbound metadata: want from=bastian chat_id=777, got from=%s chat_id=%d", inbound.From, inbound.ChatID)
	}

	// 2. Outbound: send a message; herald should call the mock's sendMessage and ack.
	out, _ := protocol.Encode(protocol.Envelope{Op: "send", Text: "pong from harness"})
	conn.Write(out)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if !scanner.Scan() {
		t.Fatal("did not receive ack for send op")
	}
	var ack protocol.Envelope
	json.Unmarshal(scanner.Bytes(), &ack)
	if ack.Op != "ack" || !ack.OK {
		t.Errorf("ack: want op=ack ok=true, got %+v", ack)
	}

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.sentCount == 0 {
		t.Fatal("mock Telegram never received a sendMessage call")
	}
	if mock.sentText != "pong from harness" {
		t.Errorf("sendMessage text: want \"pong from harness\" got %q", mock.sentText)
	}
	if mock.sentChatID != 555 {
		t.Errorf("sendMessage chat_id: want 555 (default), got %d", mock.sentChatID)
	}
}

func dialWithRetry(t *testing.T, path string, timeout time.Duration) net.Conn {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", path)
		if err == nil {
			return conn
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket %s never became available", path)
	return nil
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
