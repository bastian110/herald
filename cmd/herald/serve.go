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

// Config holds the daemon's runtime configuration.
type Config struct {
	Token         string
	BaseURL       string
	SocketPath    string
	PollTimeout   int
	DefaultChatID int64
}

func serveCmd(args []string) int {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Println("TELEGRAM_BOT_TOKEN is required")
		return 1
	}
	cfg := Config{
		Token:         token,
		SocketPath:    envOrDefault("HERALD_SOCKET", "/tmp/herald.sock"),
		PollTimeout:   envInt("HERALD_POLL_TIMEOUT", 30),
		DefaultChatID: envInt64("HERALD_CHAT_ID", 0),
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		log.Println(err)
		return 1
	}
	return 0
}

func run(ctx context.Context, cfg Config) error {
	var client *telegram.Client
	if cfg.BaseURL == "" {
		client = telegram.NewClient(cfg.Token)
	} else {
		client = telegram.NewClientWithBase(cfg.Token, cfg.BaseURL)
	}
	poller := telegram.NewPoller(client, cfg.PollTimeout)
	r := router.New()

	send := func(chatID int64, text string) error {
		if chatID == 0 {
			chatID = cfg.DefaultChatID
		}
		return client.SendMessage(chatID, text)
	}

	srv := socket.New(cfg.SocketPath, r, send)
	updates := make(chan telegram.Update, 32)
	go poller.Run(ctx, updates)
	go broadcastUpdates(ctx, updates, r)

	log.Printf("herald listening on %s", cfg.SocketPath)
	return srv.Run(ctx)
}

func broadcastUpdates(ctx context.Context, updates <-chan telegram.Update, r *router.Router) {
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
