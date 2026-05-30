# herald

A standalone Unix socket daemon that bridges AI harnesses to messaging platforms. v1 targets Telegram; the adapter model is built to extend to WhatsApp, Signal, and others.

Harnesses connect to a Unix socket and exchange newline-delimited JSON. Herald long-polls Telegram for incoming messages and broadcasts them to all connected harnesses; outbound messages from any harness are forwarded to Telegram.

## Architecture

```
                          ┌──────────────────────────────────────────┐
                          │                 herald                    │
   Telegram Bot API       │  ┌──────────┐   ┌────────┐   ┌─────────┐  │
  ┌──────────────┐        │  │ telegram │──▶│ router │──▶│ socket  │  │
  │  getUpdates  │◀───────┼──│  poller  │   │ (fan-  │   │ server  │  │
  │  sendMessage │───────▶│  │  client  │◀──│  out)  │◀──│         │  │
  └──────────────┘        │  └──────────┘   └────────┘   └────┬────┘  │
                          └────────────────────────────────────┼──────┘
                                          /tmp/herald.sock  ◀───┘
                                                  ▲ (newline-delimited JSON)
                       ┌──────────────────────────┼──────────────────────────┐
                       │                          │                          │
                  herald-send               herald-recv              herald-pi-bridge
                  (notify, 1-shot)          (read, 1-shot)           (persistent: recv→pi→send)
                                                                            │
                                                                       pi -p "…"
```

**Three layers, each with one job:**

| Layer | Component | Responsibility |
|-------|-----------|----------------|
| Daemon | `herald` (Go) | Long-poll Telegram, broadcast inbound to all listeners, forward outbound. Pure transport — knows nothing about agents. |
| Wrappers | `herald-send` / `herald-recv` | One-shot primitives hiding the raw socket + JSON. For notifications and simple scripts. |
| Bridge | `herald-pi-bridge` | Persistent listener that drives an agent (`pi`): `recv → pi → send`, with session persistence and `/new` reset. |

Internally the daemon is four focused Go packages: `internal/telegram` (poller + client), `internal/router` (broadcast fan-out), `internal/socket` (Unix socket server), `internal/protocol` (JSON envelope). `cmd/herald` wires them together.

**Message flows:**
- *Outbound* — harness writes `{"op":"send",…}` → socket server → telegram client `sendMessage` → ack back to that harness.
- *Inbound* — poller `getUpdates` → router broadcasts `{"op":"message",…}` to every connected listener.

> Herald broadcasts every inbound message to **all** listeners, so run **one** responding bridge at a time. Multiple agents would need routing (e.g. a `/pi` vs `/claude` prefix) — not implemented yet.

## Build

```bash
go build -o herald ./cmd/herald
```

## Startup commands

Configure once — copy `.env.example` to `.env` and fill in your token + chat_id:

```bash
cp .env.example .env
$EDITOR .env        # set TELEGRAM_BOT_TOKEN and HERALD_CHAT_ID
```

**Manual run (dev / one-off):**

```bash
go build -o herald ./cmd/herald
set -a && . ./.env && set +a    # load .env into the environment
./herald &                       # 1. start the daemon
bin/herald-pi-bridge &           # 2. (optional) start the pi bridge
```

> ⚠️ Run **exactly one** `herald` instance per bot token. Two daemons both
> long-poll the same bot and steal each other's updates. The systemd unit below
> enforces this — prefer it for anything long-running.

**Service run (recommended, survives logout + boot):**

```bash
go build -o herald ./cmd/herald
mkdir -p ~/.config/systemd/user
cp deploy/herald.service deploy/herald-pi-bridge.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now herald herald-pi-bridge
loginctl enable-linger "$USER"

# manage / observe
systemctl --user status herald herald-pi-bridge
journalctl --user -u herald -f
systemctl --user restart herald          # after rebuilding the binary
systemctl --user stop herald herald-pi-bridge
```

**Environment variables** (read from `.env` by both the binary and the systemd unit):

| Variable | Default | Description |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | *(required)* | Bot token from @BotFather |
| `HERALD_SOCKET` | `/tmp/herald.sock` | Unix socket path |
| `HERALD_POLL_TIMEOUT` | `30` | Telegram long-poll timeout (seconds) |
| `HERALD_CHAT_ID` | `0` | Default chat_id for sends with no explicit chat_id |

Bridge-only variables (`herald-pi-bridge`):

| Variable | Default | Description |
|---|---|---|
| `HERALD_PI_SESSION_DIR` | `~/.herald/pi-sessions` | Isolated pi session storage (keeps `pi -c` scoped to the bridge) |
| `HERALD_PI_WORKDIR` | `$HOME` | Working directory pi runs in |

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
socket, run your agent, `herald-send` the reply. systemd user units for both the
daemon and the bridge live in `deploy/` — see [Startup commands](#startup-commands).

## Test

```bash
go test ./...
```
