# Herald Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `herald`, a Go daemon that bridges AI harnesses to Telegram via Unix socket, supporting bidirectional JSON-line messaging with long-poll Telegram updates broadcast to all connected harnesses.

**Architecture:** Herald runs as a persistent daemon exposing a Unix socket at `/tmp/herald.sock`. Harnesses connect and exchange JSON lines — writing `send` ops and reading `message` ops. Internally, a long-poll loop fetches Telegram updates and a broadcast router fans them to all connected harnesses.

**Tech Stack:** Go 1.21+, standard library only (net, net/http, encoding/json, os/signal). No external dependencies.

---

## File Map

| File | Responsibility |
|---|---|
| `go.mod` | Module definition |
| `internal/protocol/message.go` | JSON envelope type + encode/decode |
| `internal/protocol/message_test.go` | Protocol roundtrip tests |
| `internal/router/router.go` | Broadcast router (subscribe/unsubscribe/broadcast) |
| `internal/router/router_test.go` | Router unit tests |
| `internal/telegram/client.go` | Telegram HTTP client + SendMessage |
| `internal/telegram/client_test.go` | Client unit tests (mock HTTP server) |
| `internal/telegram/poller.go` | Long-poll getUpdates loop + Update types |
| `internal/telegram/poller_test.go` | Poller unit tests (mock HTTP server) |
| `internal/socket/server.go` | Unix socket server + per-connection handler |
| `internal/socket/server_test.go` | Socket integration tests |
| `cmd/herald/main.go` | Daemon entry point, wiring, signal handling |

---

### Task 1: Go module scaffolding

**Files:**
- Create: `go.mod`
- Create: `cmd/herald/main.go`

- [ ] **Step 1: Initialize the Go module**

```bash
cd ~/Projects/herald
go mod init github.com/bastian110/herald
```

Expected output: `go: creating new go.mod: module github.com/bastian110/herald`

- [ ] **Step 2: Create directory structure**

```bash
mkdir -p internal/protocol internal/router internal/telegram internal/socket cmd/herald
```

- [ ] **Step 3: Create stub main.go**

Create `cmd/herald/main.go`:

```go
package main

func main() {}
```

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 5: Commit**

```bash
git add go.mod cmd/herald/main.go internal/
git commit -m "chore: scaffold Go module and directory structure"
```

---

### Task 2: Protocol envelope

**Files:**
- Create: `internal/protocol/message.go`
- Create: `internal/protocol/message_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/protocol/message_test.go`:

```go
package protocol_test

import (
	"testing"

	"github.com/bastian110/herald/internal/protocol"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	original := protocol.Envelope{
		Op:     "message",
		Text:   "hello",
		From:   "bastian",
		ChatID: 123456,
	}
	encoded, err := protocol.Encode(original)
	if err != nil {
		t.Fatal(err)
	}
	if encoded[len(encoded)-1] != '\n' {
		t.Error("encoded message must end with newline")
	}
	decoded, err := protocol.Decode(encoded[:len(encoded)-1])
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Op != original.Op {
		t.Errorf("Op: want %q got %q", original.Op, decoded.Op)
	}
	if decoded.Text != original.Text {
		t.Errorf("Text: want %q got %q", original.Text, decoded.Text)
	}
	if decoded.From != original.From {
		t.Errorf("From: want %q got %q", original.From, decoded.From)
	}
	if decoded.ChatID != original.ChatID {
		t.Errorf("ChatID: want %d got %d", original.ChatID, decoded.ChatID)
	}
}

func TestEncodeAck(t *testing.T) {
	b, err := protocol.Encode(protocol.Envelope{Op: "ack", OK: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := protocol.Decode(b[:len(b)-1])
	if err != nil {
		t.Fatal(err)
	}
	if got.Op != "ack" || !got.OK {
		t.Errorf("want ack ok=true, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/protocol/...
```

Expected: `cannot find package` or `undefined: protocol.Envelope`.

- [ ] **Step 3: Implement the protocol envelope**

Create `internal/protocol/message.go`:

```go
package protocol

import "encoding/json"

// Envelope is the JSON line exchanged between herald and harnesses.
// op values: "send" (harness→herald), "message" (herald→harness),
//            "ack" (herald→harness after send), "error" (herald→harness on failure).
type Envelope struct {
	Op     string `json:"op"`
	Text   string `json:"text,omitempty"`
	From   string `json:"from,omitempty"`
	ChatID int64  `json:"chat_id,omitempty"`
	OK     bool   `json:"ok,omitempty"`
	Error  string `json:"message,omitempty"`
}

// Encode serialises e to JSON and appends a newline delimiter.
func Encode(e Envelope) ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Decode deserialises a single JSON line (without trailing newline).
func Decode(data []byte) (Envelope, error) {
	var e Envelope
	return e, json.Unmarshal(data, &e)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/protocol/... -v
```

Expected:
```
--- PASS: TestEncodeDecodeRoundtrip (0.00s)
--- PASS: TestEncodeAck (0.00s)
PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/protocol/
git commit -m "feat: add protocol envelope encode/decode"
```

---

### Task 3: Broadcast router

**Files:**
- Create: `internal/router/router.go`
- Create: `internal/router/router_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/router/router_test.go`:

```go
package router_test

import (
	"testing"
	"time"

	"github.com/bastian110/herald/internal/router"
)

func TestBroadcastToAllSubscribers(t *testing.T) {
	r := router.New()
	ch1 := r.Subscribe()
	ch2 := r.Subscribe()
	defer r.Unsubscribe(ch1)
	defer r.Unsubscribe(ch2)

	msg := []byte(`{"op":"message","text":"hello"}` + "\n")
	r.Broadcast(msg)

	for i, ch := range []chan []byte{ch1, ch2} {
		select {
		case got := <-ch:
			if string(got) != string(msg) {
				t.Errorf("subscriber %d: want %q got %q", i, msg, got)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d: timeout waiting for broadcast", i)
		}
	}
}

func TestUnsubscribeRemovesChannel(t *testing.T) {
	r := router.New()
	ch := r.Subscribe()
	r.Unsubscribe(ch)
	// broadcasting to empty router must not panic
	r.Broadcast([]byte(`{"op":"message"}` + "\n"))
}

func TestBroadcastDropsWhenBufferFull(t *testing.T) {
	r := router.New()
	ch := r.Subscribe()
	defer r.Unsubscribe(ch)

	// fill the buffer (capacity 16) without reading
	for i := 0; i < 20; i++ {
		r.Broadcast([]byte(`{"op":"message"}` + "\n"))
	}
	// must not block or panic — excess messages are dropped
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/router/...
```

Expected: `undefined: router.New`.

- [ ] **Step 3: Implement the router**

Create `internal/router/router.go`:

```go
package router

import "sync"

// Router broadcasts byte slices to all subscribed channels.
type Router struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func New() *Router {
	return &Router{clients: make(map[chan []byte]struct{})}
}

// Subscribe returns a new channel that will receive all future broadcasts.
// Buffer size 16: slow harnesses drop messages rather than blocking the daemon.
func (r *Router) Subscribe() chan []byte {
	ch := make(chan []byte, 16)
	r.mu.Lock()
	r.clients[ch] = struct{}{}
	r.mu.Unlock()
	return ch
}

// Unsubscribe removes ch from the broadcast set and closes it.
func (r *Router) Unsubscribe(ch chan []byte) {
	r.mu.Lock()
	delete(r.clients, ch)
	r.mu.Unlock()
	close(ch)
}

// Broadcast sends msg to every subscribed channel. Drops silently if a channel's buffer is full.
func (r *Router) Broadcast(msg []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for ch := range r.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/router/... -v
```

Expected:
```
--- PASS: TestBroadcastToAllSubscribers (0.00s)
--- PASS: TestUnsubscribeRemovesChannel (0.00s)
--- PASS: TestBroadcastDropsWhenBufferFull (0.00s)
PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/router/
git commit -m "feat: add broadcast router"
```

---

### Task 4: Telegram HTTP client

**Files:**
- Create: `internal/telegram/client.go`
- Create: `internal/telegram/client_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/telegram/client_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/telegram/... -run TestSendMessage
```

Expected: `undefined: telegram.NewClientWithBase`.

- [ ] **Step 3: Implement the Telegram client**

Create `internal/telegram/client.go`:

```go
package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client handles outbound Telegram API calls.
type Client struct {
	token   string
	baseURL string
	httpCl  *http.Client
}

// NewClient creates a Client targeting the real Telegram API.
func NewClient(token string) *Client {
	return NewClientWithBase(token, "https://api.telegram.org")
}

// NewClientWithBase creates a Client with a custom base URL (used in tests).
func NewClientWithBase(token, baseURL string) *Client {
	return &Client{
		token:   token,
		baseURL: baseURL,
		httpCl:  &http.Client{Timeout: 10 * time.Second},
	}
}

type sendMessageReq struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

// SendMessage posts a text message to the given Telegram chat.
func (c *Client) SendMessage(chatID int64, text string) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, c.token)
	body, err := json.Marshal(sendMessageReq{ChatID: chatID, Text: text})
	if err != nil {
		return err
	}
	resp, err := c.httpCl.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram sendMessage: HTTP %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/telegram/... -run TestSendMessage -v
```

Expected:
```
--- PASS: TestSendMessageCallsCorrectEndpoint (0.00s)
--- PASS: TestSendMessageReturnsErrorOnNon200 (0.00s)
PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/telegram/client.go internal/telegram/client_test.go
git commit -m "feat: add Telegram HTTP client with SendMessage"
```

---

### Task 5: Telegram poller

**Files:**
- Modify: `internal/telegram/client.go` (add `baseURL` accessor for poller)
- Create: `internal/telegram/poller.go`
- Create: `internal/telegram/poller_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/telegram/poller_test.go`:

```go
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
	<-updates                            // consume first update
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/telegram/... -run TestPoller
```

Expected: `undefined: telegram.NewPoller`.

- [ ] **Step 3: Implement the poller**

Create `internal/telegram/poller.go`:

```go
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Update is a Telegram Bot API update object.
type Update struct {
	UpdateID int      `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

// Message is a Telegram message.
type Message struct {
	MessageID int     `json:"message_id"`
	Text      string  `json:"text"`
	Chat      Chat    `json:"chat"`
	From      *User   `json:"from,omitempty"`
}

// Chat holds the chat ID.
type Chat struct {
	ID int64 `json:"id"`
}

// User holds sender info.
type User struct {
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// Poller long-polls the Telegram getUpdates endpoint and emits updates to a channel.
type Poller struct {
	client  *Client
	timeout int   // Telegram long-poll timeout in seconds
	offset  int
	httpCl  *http.Client
}

// NewPoller creates a Poller. timeout is the Telegram server-side long-poll duration in seconds.
func NewPoller(client *Client, timeout int) *Poller {
	return &Poller{
		client:  client,
		timeout: timeout,
		// HTTP client timeout must exceed the Telegram long-poll timeout.
		httpCl: &http.Client{Timeout: time.Duration(timeout+5) * time.Second},
	}
}

func (p *Poller) getUpdates(ctx context.Context) ([]Update, error) {
	url := fmt.Sprintf(
		"%s/bot%s/getUpdates?timeout=%d&offset=%d",
		p.client.baseURL, p.client.token, p.timeout, p.offset,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.httpCl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Result) > 0 {
		p.offset = result.Result[len(result.Result)-1].UpdateID + 1
	}
	return result.Result, nil
}

// Run polls Telegram in a loop until ctx is cancelled, sending updates to the channel.
// On error, it retries with exponential backoff (1s → 2s → … → 60s cap).
func (p *Poller) Run(ctx context.Context, updates chan<- Update) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		batch, err := p.getUpdates(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				if backoff < 60*time.Second {
					backoff *= 2
				}
				continue
			}
		}
		backoff = time.Second
		for _, u := range batch {
			select {
			case updates <- u:
			case <-ctx.Done():
				return
			}
		}
	}
}
```

- [ ] **Step 4: Run all telegram tests**

```bash
go test ./internal/telegram/... -v
```

Expected: all four tests pass (`TestSendMessage*`, `TestPoller*`).

- [ ] **Step 5: Commit**

```bash
git add internal/telegram/poller.go internal/telegram/poller_test.go
git commit -m "feat: add Telegram long-poll poller"
```

---

### Task 6: Unix socket server

**Files:**
- Create: `internal/socket/server.go`
- Create: `internal/socket/server_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/socket/server_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/socket/...
```

Expected: `undefined: socket.New`.

- [ ] **Step 3: Implement the socket server**

Create `internal/socket/server.go`:

```go
package socket

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/bastian110/herald/internal/protocol"
	"github.com/bastian110/herald/internal/router"
)

// Server accepts Unix socket connections from harnesses.
type Server struct {
	path   string
	router *router.Router
	send   func(chatID int64, text string) error
}

// New creates a Server. send is called when a harness writes a "send" op.
func New(path string, r *router.Router, send func(int64, string) error) *Server {
	return &Server{path: path, router: r, send: send}
}

// Run listens on the Unix socket until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	os.Remove(s.path)
	l, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("herald socket listen %s: %w", s.path, err)
	}
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("herald socket accept: %w", err)
			}
		}
		go s.handle(ctx, conn)
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	ch := s.router.Subscribe()
	defer s.router.Unsubscribe(ch)

	// Push inbound Telegram messages to this harness.
	go func() {
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				conn.Write(msg) //nolint:errcheck
			case <-ctx.Done():
				return
			}
		}
	}()

	// Read outbound send ops from this harness.
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var env protocol.Envelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err != nil {
			continue
		}
		if env.Op != "send" {
			continue
		}
		err := s.send(env.ChatID, env.Text)
		var ack protocol.Envelope
		if err != nil {
			ack = protocol.Envelope{Op: "error", Error: err.Error()}
		} else {
			ack = protocol.Envelope{Op: "ack", OK: true}
		}
		b, _ := protocol.Encode(ack)
		conn.Write(b) //nolint:errcheck
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/socket/... -v
```

Expected:
```
--- PASS: TestSendOpForwardsToSendFn (0.0xs)
--- PASS: TestInboundBroadcastReachesHarness (0.0xs)
PASS
```

- [ ] **Step 5: Run all tests**

```bash
go test ./...
```

Expected: all packages pass.

- [ ] **Step 6: Commit**

```bash
git add internal/socket/
git commit -m "feat: add Unix socket server with bidirectional JSON-line protocol"
```

---

### Task 7: Main daemon wiring

**Files:**
- Modify: `cmd/herald/main.go`

- [ ] **Step 1: Implement main.go**

Replace `cmd/herald/main.go`:

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/bastian110/herald/internal/protocol"
	"github.com/bastian110/herald/internal/router"
	"github.com/bastian110/herald/internal/socket"
	"github.com/bastian110/herald/internal/telegram"
)

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}

	socketPath := envOrDefault("HERALD_SOCKET", "/tmp/herald.sock")
	pollTimeout := envInt("HERALD_POLL_TIMEOUT", 30)
	defaultChatID := envInt64("HERALD_CHAT_ID", 0)

	client := telegram.NewClient(token)
	poller := telegram.NewPoller(client, pollTimeout)
	r := router.New()

	send := func(chatID int64, text string) error {
		if chatID == 0 {
			chatID = defaultChatID
		}
		return client.SendMessage(chatID, text)
	}

	srv := socket.New(socketPath, r, send)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	updates := make(chan telegram.Update, 32)
	go poller.Run(ctx, updates)

	go func() {
		for {
			select {
			case u, ok := <-updates:
				if !ok {
					return
				}
				if u.Message == nil || u.Message.Text == "" {
					continue
				}
				from := ""
				if u.Message.From != nil {
					from = u.Message.From.Username
					if from == "" {
						from = u.Message.From.FirstName
					}
				}
				env := protocol.Envelope{
					Op:     "message",
					Text:   u.Message.Text,
					From:   from,
					ChatID: u.Message.Chat.ID,
				}
				b, err := protocol.Encode(env)
				if err != nil {
					log.Printf("encode error: %v", err)
					continue
				}
				r.Broadcast(b)
			case <-ctx.Done():
				return
			}
		}
	}()

	log.Printf("herald listening on %s", socketPath)
	if err := srv.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
```

- [ ] **Step 2: Build the binary**

```bash
go build -o herald ./cmd/herald
```

Expected: `./herald` binary created, no errors.

- [ ] **Step 3: Run all tests one final time**

```bash
go test ./...
```

Expected: all packages pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/herald/main.go
git commit -m "feat: wire main daemon — socket server + Telegram poller + router"
```

---

### Task 8: Smoke test

**Manual verification** — no bot token needed for socket protocol; bot token needed for Telegram roundtrip.

- [ ] **Step 1: Test socket protocol locally (no Telegram needed)**

Terminal 1 — start herald with a fake token (it will fail to connect to Telegram, which is fine for socket testing):

```bash
TELEGRAM_BOT_TOKEN=fake HERALD_CHAT_ID=123 ./herald
```

Terminal 2 — connect with socat and send a message:

```bash
echo '{"op":"send","text":"hello","chat_id":123}' | socat - UNIX-CONNECT:/tmp/herald.sock
```

Expected output in Terminal 2:
```json
{"op":"error","message":"telegram sendMessage: HTTP 401"}
```
(401 because token is fake — the socket protocol itself works correctly.)

- [ ] **Step 2: Test full Telegram roundtrip**

Requires a real bot token. Set env vars and run:

```bash
export TELEGRAM_BOT_TOKEN=<your-token>
export HERALD_CHAT_ID=<your-chat-id>
./herald &

# In another terminal, listen for incoming messages:
socat - UNIX-CONNECT:/tmp/herald.sock &

# Send a message to yourself via Telegram, verify it appears in socat output.
# Send a message from socat, verify it arrives in Telegram.
echo '{"op":"send","text":"hello from herald"}' | socat - UNIX-CONNECT:/tmp/herald.sock
```

- [ ] **Step 3: Cross-compile for Raspberry Pi (ARM64)**

```bash
GOOS=linux GOARCH=arm64 go build -o herald-arm64 ./cmd/herald
file herald-arm64
```

Expected: `herald-arm64: ELF 64-bit LSB executable, ARM aarch64`

- [ ] **Step 4: Final commit**

```bash
git add -u
git commit -m "chore: final build verification"
git push origin master
```

---

## Environment Variables Reference

| Variable | Default | Description |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | *(required)* | Bot token from @BotFather |
| `HERALD_SOCKET` | `/tmp/herald.sock` | Unix socket path |
| `HERALD_POLL_TIMEOUT` | `30` | Telegram long-poll timeout in seconds |
| `HERALD_CHAT_ID` | `0` | Default chat_id for `send` ops without an explicit `chat_id` |
