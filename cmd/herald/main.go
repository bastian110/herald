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
