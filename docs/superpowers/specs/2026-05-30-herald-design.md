# Herald — Design Spec

**Date:** 2026-05-30  
**Status:** Approved

## Overview

Herald is a standalone Unix socket daemon that provides bidirectional communication between AI harnesses (Claude Code, Codex, custom agents) and messaging platforms. v1 targets Telegram only; the adapter model is designed for future extension to WhatsApp, Signal, etc.

## Architecture

```
Harness(es)
    │  Unix socket  (/tmp/herald.sock)
    ▼
Herald Daemon
    │  HTTP long-poll  (getUpdates?timeout=30)
    ▼
Telegram Bot API
```

Herald runs persistently as a daemon. Any number of harnesses connect to its Unix socket simultaneously. All connected harnesses receive all inbound Telegram messages (broadcast). Outbound messages from any harness are forwarded to Telegram.

## Components

```
herald/
  cmd/herald/main.go       # entry point, daemon lifecycle, signal handling
  internal/socket/         # Unix socket server, connection management
  internal/telegram/       # Telegram adapter: long-poll loop + sendMessage
  internal/router/         # broadcast router: TG→harnesses, harness→TG
```

**Language:** Go — single static binary, strong concurrency primitives, easy cross-compilation for Raspberry Pi (ARM64).

## Protocol

JSON lines (newline-delimited) over the Unix socket.

### Outbound (harness → Telegram)
```json
{"op":"send","text":"your message"}
```

### Inbound (Telegram → harness)
```json
{"op":"message","text":"reply text","from":"bastian","chat_id":"123456"}
```

### Acknowledgement (herald → harness, after send)
```json
{"op":"ack","ok":true}
```

### Error (herald → harness)
```json
{"op":"error","message":"telegram unavailable"}
```

## Data Flow

**Outbound path:**
1. Harness writes JSON line to socket
2. Herald router receives, passes to Telegram adapter
3. Adapter calls `sendMessage`
4. Herald writes `ack` back to the sending harness

**Inbound path:**
1. Telegram adapter long-polls `getUpdates?timeout=30`
2. On update received, adapter passes to router
3. Router broadcasts JSON line to all connected harness sockets
4. Each harness reads from its socket (blocking read)

## Error Handling

**Herald-side:**
- Harness disconnects mid-session → remove from broadcast list silently, no crash
- Telegram unreachable → exponential backoff retry (1s → 2s → 4s → … → 60s cap)
- Invalid bot token → log error and exit(1) at startup before accepting connections

**Harness-side:**
- Herald not running → connection refused; harness is responsible for clear user-facing error
- Socket lost mid-session → harness must reconnect; herald does not buffer or replay

**Message persistence:** None in v1. If no harness is connected when a Telegram message arrives, the message is delivered to nobody. Acceptable for current single-agent use case.

## Configuration

Herald reads from environment variables at startup:

| Variable | Description |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Bot token from @BotFather |
| `HERALD_SOCKET` | Socket path (default: `/tmp/herald.sock`) |
| `HERALD_POLL_TIMEOUT` | Long-poll timeout in seconds (default: `30`) |

## Connecting from a Harness

**Bash / socat:**
```bash
# Send a message
echo '{"op":"send","text":"hello"}' | socat - UNIX-CONNECT:/tmp/herald.sock

# Receive next message (blocking)
socat - UNIX-CONNECT:/tmp/herald.sock
```

**Python:**
```python
import socket, json

sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.connect("/tmp/herald.sock")
sock.sendall(json.dumps({"op": "send", "text": "hello"}).encode() + b"\n")
response = json.loads(sock.recv(4096))
```

## Testing

- Unit: Telegram adapter mocked via interface, router logic tested with in-memory connections
- Integration: real Telegram bot token in CI env, send+receive roundtrip test
- Manual: `socat` as a harness to smoke-test the socket protocol

## Out of Scope (v1)

- Message persistence / queuing
- Multi-harness routing by chat_id or subscription
- WhatsApp, Signal, or other platforms
- Authentication between harness and herald socket
- Web/HTTP interface
