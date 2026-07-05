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
                 herald send                herald recv              herald-pi-bridge
                 (notify, 1-shot)           (read; --follow streams) (persistent: recv→pi→send)
                                                                            │
                                                                       pi -p "…"
```

**Three layers, each with one job:**

| Layer | Component | Responsibility |
|-------|-----------|----------------|
| Daemon | `herald serve` | Long-poll Telegram, broadcast inbound to all listeners, forward outbound. Pure transport — knows nothing about agents. |
| Primitives | `herald send` / `herald recv` | Binary subcommands hiding the raw socket + JSON. For notifications and as building blocks for bridges. No `socat`/`jq` needed. |
| Bridge | `herald-pi-bridge` | Harness-specific listener that drives an agent (`pi`): `recv → pi → send`, with session persistence and `/new` reset. |

One Go binary carries everything generic (`serve`/`send`/`recv`); each **bridge stays specific to its harness** but is a trivial script over `herald recv --follow` + `herald send`.

Internally the daemon is five focused Go packages: `internal/telegram` (poller + client), `internal/router` (broadcast fan-out), `internal/socket` (Unix socket server), `internal/protocol` (JSON envelope), `internal/client` (socket primitives for the subcommands). `cmd/herald` dispatches the subcommands.

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
export PATH="$PWD:$PWD/bin:$PATH"   # so `herald` and the bridge are on PATH
set -a && . ./.env && set +a        # load .env into the environment
herald serve &                       # 1. start the daemon
herald-pi-bridge &                   # 2. (optional) start the pi bridge
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
| `HERALD_PI_SESSION_DIR` | `~/.herald/pi-sessions` | Isolated pi session storage for bridge conversations |
| `HERALD_PI_WORKDIR` | `$HOME` | Working directory pi runs in |
| `HERALD_PI_LOCK_FILE` | `/tmp/herald-pi-bridge.lock` | Process lock so only one responding pi bridge runs |
| `HERALD_PI_ACTIVE_SESSION_FILE` | `$HERALD_PI_SESSION_DIR/.herald-active-session` | Active pi conversation id marker used by `/new` and `/continue` |
| `HERALD_PI_QUEUE_DIR` | `/tmp/herald-pi-bridge.queue` | Pending-message queue for sequential pi runs |
| `HERALD_PI_RUN_PID_FILE` | `/tmp/herald-pi-bridge.pi.pid` | PID file for the active pi process, used by `/new` and `/reload` |
| `HERALD_WHISPER_ROOT` | `~/Projects/oss/whisper.cpp` | whisper.cpp install root used for Telegram voice notes |
| `HERALD_WHISPER_MODEL` | `$HERALD_WHISPER_ROOT/models/ggml-base.bin` | Whisper model path |
| `HERALD_PI_VOICE_LANG` | `auto` | Whisper language override for voice note transcription |

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

## Connecting a harness

Herald is just transport. The binary's `send`/`recv` subcommands are the
primitives; a harness-specific bridge sits on top.

### Primitives (subcommands)

No `socat`/`jq` — the binary speaks the protocol itself.

```bash
herald send "build finished ✅"        # notify the default chat
herald send --chat-id 123456 "done"    # notify a specific chat_id
text=$(herald recv)                     # block for the next inbound message
herald recv --json                      # raw envelope (text + from + chat_id)
herald recv --timeout 60                # give up after 60s (exit 1)
herald recv --follow                    # stream every message (for bridges)
```

`herald recv` (one-shot) opens a fresh connection per call, so messages arriving
between calls are not buffered. A durable listener holds **one** connection with
`herald recv --follow` — that is what a bridge does.

### Bridge (drive an agent from Telegram)

`bin/herald-pi-bridge` follows the stream and, for each inbound message, runs the
[`pi`](https://pi.dev) agent and sends the reply back — the whole bridge is three
lines over the primitives:

```bash
herald recv --follow | while IFS= read -r text; do
  reply=$(pi --session-dir "$S" -c -p "$text" </dev/null 2>&1)
  herald send "$reply"
done
```

```
User TG → herald → herald-pi-bridge → pi -p "…" → herald → User TG
```

- The pi conversation **persists across messages** (context carries over),
  using an isolated session dir and an active-session marker scoped to this
  bridge.
- Sending **`/new`** from Telegram resets the agent context only — the Telegram
  chat itself is untouched — and replies with the closed conversation ID.
- Sending **`/continue <conversation-id>`** resumes a previous pi conversation.
- Telegram **voice notes** are downloaded, transcribed with `whisper.cpp`, then
  appended to the prompt before `pi` runs.
- Telegram **photos/images** are downloaded and passed to `pi` as `@image`
  file arguments, with the caption used as the prompt when present. Telegram
  albums are buffered briefly, then sent to `pi` in one multi-image prompt.
- pi runs **on demand** (one process per message), with inbound messages queued
  and processed sequentially; only herald runs continuously.

```bash
herald serve &           # daemon must be running first
herald-pi-bridge         # then start the bridge
```

> Herald broadcasts every inbound message to **all** connected listeners, so run
> **one** bridge (= one responding agent) at a time. Driving several agents would
> need message routing (e.g. a `/pi` vs `/claude` prefix) — not implemented yet.
> `herald-pi-bridge` also takes a process lock and exits if another copy is
> already running, which prevents duplicate agent replies from accidental double
> starts.

Writing a bridge for another harness is the same shape: `herald recv --follow` →
run your agent → `herald send`. The bridge stays specific to that harness's
commands; only the plumbing is shared. systemd user units for both the daemon and
the bridge live in `deploy/` — see [Startup commands](#startup-commands).

### Raw protocol (any language)

The wire format is newline-delimited JSON on the Unix socket, so anything can
speak it directly:

```python
import socket, json
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.connect("/tmp/herald.sock")
s.sendall(json.dumps({"op": "send", "text": "hello"}).encode() + b"\n")
print(s.recv(4096))
```

## Test

```bash
go test ./...
```
