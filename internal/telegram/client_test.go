package telegram_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bastian110/herald/internal/telegram"
)

func TestSendMessageCallsCorrectEndpoint(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	client := telegram.NewClientWithBase("mytoken", srv.URL)
	err := client.SendMessage(42, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/botmytoken/sendMessage" {
		t.Errorf("path: want /botmytoken/sendMessage got %s", gotPath)
	}
	if gotBody["text"] != "hello world" {
		t.Errorf("text: want \"hello world\" got %v", gotBody["text"])
	}
	if gotBody["chat_id"] != float64(42) {
		t.Errorf("chat_id: want 42 got %v", gotBody["chat_id"])
	}
}

func TestSendMessageReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := telegram.NewClientWithBase("bad-token", srv.URL)
	err := client.SendMessage(1, "x")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestSendMessageSplitsLongText(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		got = append(got, body["text"].(string))
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	client := telegram.NewClientWithBase("tok", srv.URL)
	msg := "line1\nline2\né"
	for len([]rune(msg)) <= 4096 {
		msg += "x"
	}
	if err := client.SendMessage(7, msg); err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 {
		t.Fatalf("want split into multiple chunks, got %d", len(got))
	}
	for i, chunk := range got {
		if len([]rune(chunk)) > 4096 {
			t.Fatalf("chunk %d too long: %d runes", i, len([]rune(chunk)))
		}
	}
}
