package main

import (
	"context"
	"testing"
	"time"

	"github.com/bastian110/herald/internal/protocol"
	"github.com/bastian110/herald/internal/router"
	"github.com/bastian110/herald/internal/telegram"
)

func TestBroadcastUpdatesImage(t *testing.T) {
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
		Caption: "décris ça",
		Photo: []telegram.PhotoSize{
			{FileID: "small-photo-id", Width: 90, Height: 90, FileSize: 1},
			{FileID: "large-photo-id", Width: 1280, Height: 720, FileSize: 2},
		},
	}}

	select {
	case raw := <-ch:
		env, err := protocol.Decode(raw[:len(raw)-1])
		if err != nil {
			t.Fatal(err)
		}
		if env.Kind != "image" {
			t.Fatalf("kind: want image got %q", env.Kind)
		}
		if env.FileID != "large-photo-id" {
			t.Fatalf("file_id: want large-photo-id got %q", env.FileID)
		}
		if env.MimeType != "image/jpeg" {
			t.Fatalf("mime_type: want image/jpeg got %q", env.MimeType)
		}
		if env.Text != "décris ça" {
			t.Fatalf("text/caption: want image caption got %q", env.Text)
		}
		if env.ChatID != 42 {
			t.Fatalf("chat_id: want 42 got %d", env.ChatID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for broadcasted image update")
	}
}

func TestBroadcastUpdatesImageAlbum(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := router.New()
	ch := r.Subscribe()
	defer r.Unsubscribe(ch)

	updates := make(chan telegram.Update, 2)
	go broadcastUpdates(ctx, updates, r)

	updates <- telegram.Update{Message: &telegram.Message{
		Chat:         telegram.Chat{ID: 42},
		Caption:      "compare ces images",
		MediaGroupID: "album-1",
		Photo:        []telegram.PhotoSize{{FileID: "photo-1", Width: 800, Height: 600}},
	}}
	updates <- telegram.Update{Message: &telegram.Message{
		Chat:         telegram.Chat{ID: 42},
		MediaGroupID: "album-1",
		Photo:        []telegram.PhotoSize{{FileID: "photo-2", Width: 800, Height: 600}},
	}}

	select {
	case raw := <-ch:
		env, err := protocol.Decode(raw[:len(raw)-1])
		if err != nil {
			t.Fatal(err)
		}
		if env.Kind != "image" {
			t.Fatalf("kind: want image got %q", env.Kind)
		}
		want := []string{"photo-1", "photo-2"}
		if len(env.FileIDs) != len(want) {
			t.Fatalf("file_ids: want %v got %v", want, env.FileIDs)
		}
		for i := range want {
			if env.FileIDs[i] != want[i] {
				t.Fatalf("file_ids: want %v got %v", want, env.FileIDs)
			}
		}
		if env.Text != "compare ces images" {
			t.Fatalf("text/caption: want album caption got %q", env.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for broadcasted image album")
	}
}

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
