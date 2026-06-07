package main

import (
	"context"
	"testing"
	"time"

	"github.com/bastian110/herald/internal/protocol"
	"github.com/bastian110/herald/internal/router"
	"github.com/bastian110/herald/internal/telegram"
)

func TestBroadcastUpdatesVoice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := router.New()
	ch := r.Subscribe()
	defer r.Unsubscribe(ch)

	updates := make(chan telegram.Update, 1)
	go broadcastUpdates(ctx, updates, r)

	updates <- telegram.Update{Message: &telegram.Message{
		Chat:    telegram.Chat{ID: 42},
		From:    &telegram.User{Username: "bastian"},
		Caption: "résume ça",
		Voice:   &telegram.Voice{FileID: "voice-file-id", MimeType: "audio/ogg"},
	}}

	select {
	case raw := <-ch:
		env, err := protocol.Decode(raw[:len(raw)-1])
		if err != nil {
			t.Fatal(err)
		}
		if env.Kind != "voice" {
			t.Fatalf("kind: want voice got %q", env.Kind)
		}
		if env.FileID != "voice-file-id" {
			t.Fatalf("file_id: want voice-file-id got %q", env.FileID)
		}
		if env.MimeType != "audio/ogg" {
			t.Fatalf("mime_type: want audio/ogg got %q", env.MimeType)
		}
		if env.Text != "résume ça" {
			t.Fatalf("text/caption: want résumé caption got %q", env.Text)
		}
		if env.ChatID != 42 {
			t.Fatalf("chat_id: want 42 got %d", env.ChatID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for broadcasted voice update")
	}
}
