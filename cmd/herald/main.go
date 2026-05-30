package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(dispatch(os.Args[1:]))
}

func dispatch(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "serve":
		return serveCmd(args[1:])
	case "send":
		return sendCmd(args[1:])
	case "recv":
		return recvCmd(args[1:])
	case "-h", "--help", "help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "herald: unknown command %q\n\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `herald — Telegram <-> harness bridge

Usage:
  herald serve                       run the daemon
  herald send [--chat-id N] <text>   send a message
  herald recv [--follow] [--json] [--timeout S]
                                     receive inbound message(s)

Env: TELEGRAM_BOT_TOKEN, HERALD_SOCKET, HERALD_POLL_TIMEOUT, HERALD_CHAT_ID
`)
}

func socketPath() string {
	if v := os.Getenv("HERALD_SOCKET"); v != "" {
		return v
	}
	return "/tmp/herald.sock"
}
