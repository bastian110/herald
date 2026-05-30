package socket_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"github.com/bastian110/herald/internal/protocol"
	"github.com/bastian110/herald/internal/router"
	"github.com/bastian110/herald/internal/socket"
)

func startServer(t *testing.T, path string, send func(int64, string) error) (*router.Router, context.CancelFunc) {
	t.Helper()
	os.Remove(path)
	r := router.New()
	srv := socket.New(path, r, send)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	go srv.Run(ctx) //nolint:errcheck
	time.Sleep(20 * time.Millisecond) // wait for socket to be ready
	t.Cleanup(func() { cancel(); os.Remove(path) })
	return r, cancel
}

func dialUnix(t *testing.T, path string) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestSendOpForwardsToSendFn(t *testing.T) {
	var gotChatID int64
	var gotText string
	send := func(chatID int64, text string) error {
		gotChatID = chatID
		gotText = text
		return nil
	}

	_, cancel := startServer(t, "/tmp/herald-test-send.sock", send)
	defer cancel()

	conn := dialUnix(t, "/tmp/herald-test-send.sock")

	env := protocol.Envelope{Op: "send", Text: "hello", ChatID: 456}
	b, _ := json.Marshal(env)
	conn.Write(append(b, '\n'))

	// read ack
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no ack received")
	}
	var ack protocol.Envelope
	json.Unmarshal(scanner.Bytes(), &ack)
	if ack.Op != "ack" || !ack.OK {
		t.Errorf("want ack ok=true, got %+v", ack)
	}
	if gotChatID != 456 {
		t.Errorf("chatID: want 456 got %d", gotChatID)
	}
	if gotText != "hello" {
		t.Errorf("text: want hello got %q", gotText)
	}
}

func TestInboundBroadcastReachesHarness(t *testing.T) {
	r, cancel := startServer(t, "/tmp/herald-test-inbound.sock", func(int64, string) error { return nil })
	defer cancel()

	conn := dialUnix(t, "/tmp/herald-test-inbound.sock")
	time.Sleep(10 * time.Millisecond) // ensure handler goroutine has subscribed

	msg, _ := protocol.Encode(protocol.Envelope{Op: "message", Text: "world", From: "bastian", ChatID: 123})
	r.Broadcast(msg)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no inbound message received")
	}
	var received protocol.Envelope
	json.Unmarshal(scanner.Bytes(), &received)
	if received.Op != "message" || received.Text != "world" {
		t.Errorf("want op=message text=world, got %+v", received)
	}
}
