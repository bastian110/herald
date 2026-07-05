package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bastian110/herald/internal/client"
)

func sendCmd(args []string) int {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	sock := fs.String("socket", socketPath(), "herald socket path")
	chatID := fs.Int64("chat-id", 0, "target chat_id (0 = daemon default)")
	parseMode := fs.String("parse-mode", "", "Telegram format: HTML uses rich messages; Markdown/MarkdownV2 use parse_mode")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: herald send [--chat-id N] [--parse-mode HTML] <text>")
		return 2
	}
	text := fs.Arg(0)

	ack, err := client.Send(*sock, text, *chatID, *parseMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "herald send: %v\n", err)
		return 1
	}
	if ack.Op == "ack" && ack.OK {
		return 0
	}
	fmt.Fprintf(os.Stderr, "herald send: %s\n", ack.Error)
	return 1
}
