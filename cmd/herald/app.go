package main

import (
	"context"
	"log"

	"github.com/bastian110/herald/internal/protocol"
	"github.com/bastian110/herald/internal/router"
	"github.com/bastian110/herald/internal/socket"
	"github.com/bastian110/herald/internal/telegram"
)

// Config holds the daemon's runtime configuration.
type Config struct {
	Token         string
	BaseURL       string // Telegram API base; "" uses the real api.telegram.org
	SocketPath    string
	PollTimeout   int
	DefaultChatID int64
}

// run wires the Telegram client/poller, the broadcast router, and the socket
// server together, blocking until ctx is cancelled or the socket server errors.
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

// broadcastUpdates converts incoming Telegram updates into protocol envelopes
// and broadcasts them to all connected harnesses until ctx is cancelled.
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
