package client_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bastian110/herald/internal/client"
	"github.com/bastian110/herald/internal/protocol"
	"github.com/bastian110/herald/internal/router"
	"github.com/bastian110/herald/internal/socket"
)

func startDaemon(t *testing.T, sent *[]protocol.Envelope, mu *sync.Mutex) (string, *router.Router) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.sock")
	r := router.New()
	send := func(chatID int64, text string, parseMode string) error {
		mu.Lock()
		*sent = append(*sent, protocol.Envelope{Op: "send", Text: text, ChatID: chatID, ParseMode: parseMode})
		mu.Unlock()
		return nil
	}
	srv := socket.New(path, r, send)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	go srv.Run(ctx) //nolint:errcheck
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(path); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return path, r
}

func TestSendReturnsAck(t *testing.T) {
	var sent []protocol.Envelope
	var mu sync.Mutex
	path, _ := startDaemon(t, &sent, &mu)

	ack, err := client.Send(path, "hello", 42, "HTML")
	if err != nil {
		t.Fatal(err)
	}
	if ack.Op != "ack" || !ack.OK {
		t.Errorf("want ack ok, got %+v", ack)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0].Text != "hello" || sent[0].ChatID != 42 || sent[0].ParseMode != "HTML" {
		t.Errorf("daemon did not receive correct send: %+v", sent)
	}
}

func TestSendErrorWhenDaemonDown(t *testing.T) {
	_, err := client.Send(filepath.Join(t.TempDir(), "nope.sock"), "x", 0, "")
	if err == nil {
		t.Fatal("expected error when daemon not running")
	}
}

func TestRecvOneShot(t *testing.T) {
	var sent []protocol.Envelope
	var mu sync.Mutex
	path, r := startDaemon(t, &sent, &mu)

	got := make(chan protocol.Envelope, 1)
	go func() {
		_ = client.Recv(path, false, 3*time.Second, func(e protocol.Envelope) error {
			got <- e
			return nil
		})
	}()
	time.Sleep(100 * time.Millisecond) // let Recv subscribe
	msg, _ := protocol.Encode(protocol.Envelope{Op: "message", Text: "hi", ChatID: 7})
	r.Broadcast(msg)

	select {
	case e := <-got:
		if e.Text != "hi" || e.ChatID != 7 {
			t.Errorf("want text=hi chat=7, got %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for recv")
	}
}

func TestRecvFollowDeliversMultiple(t *testing.T) {
	var sent []protocol.Envelope
	var mu sync.Mutex
	path, r := startDaemon(t, &sent, &mu)

	var got []string
	var gmu sync.Mutex
	go func() {
		_ = client.Recv(path, true, 0, func(e protocol.Envelope) error {
			gmu.Lock()
			got = append(got, e.Text)
			gmu.Unlock()
			return nil
		})
	}()
	time.Sleep(100 * time.Millisecond)
	for _, txt := range []string{"a", "b", "c"} {
		msg, _ := protocol.Encode(protocol.Envelope{Op: "message", Text: txt})
		r.Broadcast(msg)
	}
	time.Sleep(300 * time.Millisecond)

	gmu.Lock()
	defer gmu.Unlock()
	if len(got) != 3 {
		t.Fatalf("want 3 messages in follow mode, got %d: %v", len(got), got)
	}
}

func TestRecvTimeout(t *testing.T) {
	var sent []protocol.Envelope
	var mu sync.Mutex
	path, _ := startDaemon(t, &sent, &mu)

	err := client.Recv(path, false, 200*time.Millisecond, func(protocol.Envelope) error { return nil })
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
