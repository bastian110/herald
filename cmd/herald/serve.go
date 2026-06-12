package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

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

const imageAlbumFlushDelay = 1500 * time.Millisecond

type imageAlbum struct {
	env   protocol.Envelope
	timer *time.Timer
}

func broadcastUpdates(ctx context.Context, updates <-chan telegram.Update, r *router.Router) {
	albums := map[string]*imageAlbum{}
	flushes := make(chan string, 32)

	flushAlbum := func(key string) {
		album := albums[key]
		if album == nil {
			return
		}
		delete(albums, key)
		if album.timer != nil {
			album.timer.Stop()
		}
		broadcastEnvelope(album.env, r)
	}

	flushAllAlbums := func() {
		for key := range albums {
			flushAlbum(key)
		}
	}

	for {
		select {
		case key := <-flushes:
			flushAlbum(key)
		case u, ok := <-updates:
			if !ok {
				flushAllAlbums()
				return
			}
			if u.Message == nil {
				continue
			}
			env, ok := envelopeFromMessage(u.Message)
			if !ok {
				continue
			}
			if env.Kind == "image" && u.Message.MediaGroupID != "" {
				key := albumKey(u.Message)
				album := albums[key]
				if album == nil {
					env.FileIDs = []string{env.FileID}
					env.FileID = ""
					albums[key] = &imageAlbum{
						env: env,
						timer: time.AfterFunc(imageAlbumFlushDelay, func() {
							select {
							case flushes <- key:
							case <-ctx.Done():
							}
						}),
					}
					continue
				}
				album.env.FileIDs = append(album.env.FileIDs, env.FileID)
				if album.env.Text == "" {
					album.env.Text = env.Text
				}
				album.timer.Reset(imageAlbumFlushDelay)
				continue
			}
			broadcastEnvelope(env, r)
		case <-ctx.Done():
			flushAllAlbums()
			return
		}
	}
}

func envelopeFromMessage(message *telegram.Message) (protocol.Envelope, bool) {
	from := ""
	if message.From != nil {
		from = message.From.Username
		if from == "" {
			from = message.From.FirstName
		}
	}
	env := protocol.Envelope{
		Op:     "message",
		Kind:   "text",
		Text:   message.Text,
		From:   from,
		ChatID: message.Chat.ID,
	}
	if message.Text != "" {
		return env, true
	}
	if message.Voice != nil && message.Voice.FileID != "" {
		env.Kind = "voice"
		env.Text = message.Caption
		env.FileID = message.Voice.FileID
		env.MimeType = message.Voice.MimeType
		return env, true
	}
	if photo := largestPhoto(message.Photo); photo.FileID != "" {
		env.Kind = "image"
		env.Text = message.Caption
		env.FileID = photo.FileID
		env.MimeType = "image/jpeg"
		return env, true
	}
	return protocol.Envelope{}, false
}

func albumKey(message *telegram.Message) string {
	return strconv.FormatInt(message.Chat.ID, 10) + ":" + message.MediaGroupID
}

func broadcastEnvelope(env protocol.Envelope, r *router.Router) {
	b, err := protocol.Encode(env)
	if err != nil {
		log.Printf("encode error: %v", err)
		return
	}
	r.Broadcast(b)
}

func largestPhoto(photos []telegram.PhotoSize) telegram.PhotoSize {
	var largest telegram.PhotoSize
	for _, photo := range photos {
		if photo.FileID == "" {
			continue
		}
		if photo.FileSize > largest.FileSize {
			largest = photo
			continue
		}
		if photo.FileSize == largest.FileSize && photo.Width*photo.Height > largest.Width*largest.Height {
			largest = photo
		}
	}
	return largest
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
