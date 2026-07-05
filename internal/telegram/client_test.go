package telegram_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	err := client.SendMessage(42, "hello world", "")
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

func TestSendMessageSendsHTMLViaRichMessageEndpoint(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	client := telegram.NewClientWithBase("tok", srv.URL)
	if err := client.SendMessage(42, "<b>ok</b>", "HTML"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/bottok/sendRichMessage" {
		t.Fatalf("path: want /bottok/sendRichMessage got %s", gotPath)
	}
	richMessage := gotBody["rich_message"].(map[string]interface{})
	if richMessage["html"] != "<b>ok</b>" {
		t.Fatalf("rich_message.html: want original HTML got %v", richMessage["html"])
	}
}

func TestSendMessagePreservesHTMLTablesInRichMessage(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	client := telegram.NewClientWithBase("tok", srv.URL)
	input := `<table>
<tr><th>Critère</th><th>Voiture</th><th>Moto</th></tr>
<tr><td>Prix d’achat</td><td>Souvent plus élevé</td><td>Souvent moins élevé</td></tr>
</table>`
	if err := client.SendMessage(42, input, "HTML"); err != nil {
		t.Fatal(err)
	}
	richMessage := gotBody["rich_message"].(map[string]interface{})
	text := richMessage["html"].(string)
	if !strings.Contains(text, "<table>") || !strings.Contains(text, "<th>Critère</th>") {
		t.Fatalf("rich HTML table should be preserved, got %q", text)
	}
}

func TestSendMessageConvertsPlainNewlinesForRichHTML(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	client := telegram.NewClientWithBase("tok", srv.URL)
	input := "<b>Reco</b>\n\n<code>fini la page blanche</code>\nVos photos deviennent une grille food cohérente."
	if err := client.SendMessage(42, input, "HTML"); err != nil {
		t.Fatal(err)
	}
	richMessage := gotBody["rich_message"].(map[string]interface{})
	text := richMessage["html"].(string)
	if strings.Contains(text, "\n") {
		t.Fatalf("rich HTML should not rely on raw newlines, got %q", text)
	}
	if !strings.Contains(text, "<b>Reco</b><br><br><code>fini la page blanche</code><br>Vos photos") {
		t.Fatalf("rich HTML should convert plain newlines to <br>, got %q", text)
	}
}

func TestSendMessageKeepsPreNewlinesForRichHTML(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	client := telegram.NewClientWithBase("tok", srv.URL)
	input := "<pre>ligne 1\nligne 2</pre>\nAprès"
	if err := client.SendMessage(42, input, "HTML"); err != nil {
		t.Fatal(err)
	}
	richMessage := gotBody["rich_message"].(map[string]interface{})
	text := richMessage["html"].(string)
	if text != "<pre>ligne 1\nligne 2</pre><br>Après" {
		t.Fatalf("rich HTML should preserve pre newlines only, got %q", text)
	}
}

func TestSendMessageFallsBackToSanitizedHTMLWhenRichFails(t *testing.T) {
	var paths []string
	var gotFallback map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/sendRichMessage") {
			http.Error(w, "rich unavailable", http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&gotFallback); err != nil {
			t.Fatal(err)
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer srv.Close()

	client := telegram.NewClientWithBase("tok", srv.URL)
	if err := client.SendMessage(42, "<b>ok</b> <div>A & B&nbsp;</div>", "HTML"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || !strings.HasSuffix(paths[0], "/sendRichMessage") || !strings.HasSuffix(paths[1], "/sendMessage") {
		t.Fatalf("want rich attempt then sendMessage fallback, got %v", paths)
	}
	if gotFallback["parse_mode"] != "HTML" {
		t.Fatalf("fallback parse_mode: want HTML got %v", gotFallback["parse_mode"])
	}
	text := gotFallback["text"].(string)
	if !strings.Contains(text, "<b>ok</b>") {
		t.Fatalf("supported bold tag should be preserved, got %q", text)
	}
	if !strings.Contains(text, "&lt;div&gt;A &amp; B&amp;nbsp;&lt;/div&gt;") {
		t.Fatalf("unsupported tags and non-parse-mode named entities should be escaped, got %q", text)
	}
}

func TestSendMessageReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad token", http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := telegram.NewClientWithBase("bad-token", srv.URL)
	err := client.SendMessage(1, "x", "")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("expected Telegram response body in error, got %v", err)
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
	if err := client.SendMessage(7, msg, ""); err != nil {
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
