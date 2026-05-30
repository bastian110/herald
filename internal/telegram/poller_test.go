package telegram_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bastian110/herald/internal/telegram"
)

func TestPollerDeliversUpdates(t *testing.T) {
	served := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !served {
			served = true
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"result": []map[string]interface{}{
					{
						"update_id": 1,
						"message": map[string]interface{}{
							"message_id": 1,
							"text":       "hello",
							"chat":       map[string]interface{}{"id": float64(999)},
							"from":       map[string]interface{}{"username": "bastian", "first_name": "Bastian"},
						},
					},
				},
			})
		} else {
			// subsequent calls: block briefly then return empty (simulates long-poll)
			time.Sleep(10 * time.Millisecond)
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "result": []interface{}{}})
		}
	}))
	defer srv.Close()

	client := telegram.NewClientWithBase("test-token", srv.URL)
	poller := telegram.NewPoller(client, 1)
	updates := make(chan telegram.Update, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go poller.Run(ctx, updates)

	select {
	case u := <-updates:
		if u.Message == nil {
			t.Fatal("expected non-nil message")
		}
		if u.Message.Text != "hello" {
			t.Errorf("text: want hello got %q", u.Message.Text)
		}
		if u.Message.Chat.ID != 999 {
			t.Errorf("chat_id: want 999 got %d", u.Message.Chat.ID)
		}
		if u.Message.From == nil || u.Message.From.Username != "bastian" {
			t.Errorf("from.username: want bastian got %v", u.Message.From)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for update from poller")
	}
}

func TestPollerAdvancesOffset(t *testing.T) {
	callCount := 0
	var gotOffsets []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		gotOffsets = append(gotOffsets, r.URL.Query().Get("offset"))
		if callCount == 1 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"result": []map[string]interface{}{
					{"update_id": 10, "message": map[string]interface{}{
						"message_id": 1, "text": "a",
						"chat": map[string]interface{}{"id": float64(1)},
					}},
				},
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "result": []interface{}{}})
		}
	}))
	defer srv.Close()

	client := telegram.NewClientWithBase("tok", srv.URL)
	poller := telegram.NewPoller(client, 1)
	updates := make(chan telegram.Update, 4)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go poller.Run(ctx, updates)
	<-updates                           // consume first update
	time.Sleep(100 * time.Millisecond)  // let second poll fire
	cancel()

	if len(gotOffsets) < 2 {
		t.Fatalf("expected at least 2 poll calls, got %d", len(gotOffsets))
	}
	if gotOffsets[0] != "0" {
		t.Errorf("first offset: want 0 got %s", gotOffsets[0])
	}
	if gotOffsets[1] != "11" {
		t.Errorf("second offset: want 11 (update_id 10 + 1) got %s", gotOffsets[1])
	}
}
