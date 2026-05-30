# herald

A standalone Unix socket daemon that bridges AI harnesses to messaging platforms. v1 targets Telegram; the adapter model is built to extend to WhatsApp, Signal, and others.

Harnesses connect to a Unix socket and exchange newline-delimited JSON. Herald long-polls Telegram for incoming messages and broadcasts them to all connected harnesses; outbound messages from any harness are forwarded to Telegram.

## Build

```bash
go build -o herald ./cmd/herald
```

## Run

```bash
export TELEGRAM_BOT_TOKEN=<token-from-@BotFather>
export HERALD_CHAT_ID=<your-chat-id>   # default chat for sends without an explicit chat_id
./herald
```

| Variable | Default | Description |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | *(required)* | Bot token from @BotFather |
| `HERALD_SOCKET` | `/tmp/herald.sock` | Unix socket path |
| `HERALD_POLL_TIMEOUT` | `30` | Telegram long-poll timeout (seconds) |
| `HERALD_CHAT_ID` | `0` | Default chat_id for sends with no explicit chat_id |

## Protocol

Newline-delimited JSON over the Unix socket.

**Send (harness → Telegram):**
```json
{"op":"send","text":"hello"}
```

**Receive (Telegram → harness):**
```json
{"op":"message","text":"reply","from":"bastian","chat_id":123456}
```

**Ack / error (herald → harness):**
```json
{"op":"ack","ok":true}
{"op":"error","message":"telegram sendMessage: HTTP 404"}
```

## Connecting

**socat:**
```bash
echo '{"op":"send","text":"hello"}' | socat - UNIX-CONNECT:/tmp/herald.sock   # send
socat - UNIX-CONNECT:/tmp/herald.sock                                         # listen
```

**Python:**
```python
import socket, json
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.connect("/tmp/herald.sock")
s.sendall(json.dumps({"op": "send", "text": "hello"}).encode() + b"\n")
print(s.recv(4096))
```

## Connecting a harness

Herald is just transport. Two layers sit on top:

```
wrappers   send one / receive one          (bin/herald-send, bin/herald-recv)
bridge     persistent recv → agent → send  (bin/herald-pi-bridge)
```

### Wrappers (primitives)

Drop-in helpers that hide the raw socket + JSON. Add `bin/` to your `PATH`.

```bash
herald-send "build finished ✅"        # notify the default chat
herald-send "done" 123456              # notify a specific chat_id
text=$(herald-recv)                     # block for the next inbound message
json=$(herald-recv --json)              # raw envelope (text + from + chat_id)
herald-recv --timeout 60                # give up after 60s (exit 1)
```

Use the wrappers for one-shot notifications. They open a fresh connection per
call, so messages arriving between calls are not buffered — for a durable
listener, hold one persistent connection (that is what the bridge does).

### Bridge (drive an agent from Telegram)

`bin/herald-pi-bridge` holds one persistent connection and, for each inbound
message, runs the [`pi`](https://pi.dev) agent and sends the reply back:

```
User TG → herald → herald-pi-bridge → pi -p "…" → herald → User TG
```

- The pi conversation **persists across messages** (context carries over),
  using an isolated session dir so `pi -c` only ever continues this bridge's
  own conversation.
- Sending **`/new`** from Telegram resets the agent context only — the Telegram
  chat itself is untouched.
- pi runs **on demand** (one process per message); only herald runs continuously.

```bash
./herald &              # daemon must be running first
bin/herald-pi-bridge    # then start the bridge
```

> Herald broadcasts every inbound message to **all** connected listeners, so run
> **one** bridge (= one responding agent) at a time. Driving several agents would
> need message routing (e.g. a `/pi` vs `/claude` prefix) — not implemented yet.

Writing a bridge for another harness is the same shape: read envelopes from the
socket, run your agent, `herald-send` the reply.

## Running as a service

systemd user units live in `deploy/`:

```bash
go build -o herald ./cmd/herald
mkdir -p ~/.config/systemd/user
cp deploy/herald.service deploy/herald-pi-bridge.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now herald herald-pi-bridge
loginctl enable-linger "$USER"          # survive logout / start on boot
journalctl --user -u herald -f          # logs
```

## Test

```bash
go test ./...
```
