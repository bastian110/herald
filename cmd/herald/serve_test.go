package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bastian110/herald/internal/protocol"
	"github.com/bastian110/herald/internal/router"
	"github.com/bastian110/herald/internal/telegram"
)

func TestLocationEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name, location, want string
	}{
		{"Paris", `{"latitude":48.8566,"longitude":2.3522}`, "latitude 48.8566, longitude 2.3522"},
		{"zero", `{"latitude":0,"longitude":0}`, "latitude 0, longitude 0"},
		{"negative", `{"latitude":-33.8688,"longitude":-151.2093}`, "latitude -33.8688, longitude -151.2093"},
		{"invalid latitude", `{"latitude":91,"longitude":0}`, ""},
		{"invalid longitude", `{"latitude":0,"longitude":-181}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var message telegram.Message
			if err := json.Unmarshal([]byte(`{"chat":{"id":42},"location":`+tc.location+`}`), &message); err != nil {
				t.Fatal(err)
			}
			env, ok := envelopeFromMessage(&message)
			if tc.want == "" {
				if ok {
					t.Fatal("invalid coordinates accepted")
				}
				return
			}
			if !ok || env.Kind != "location" || env.ChatID != 42 || !strings.Contains(env.Text, tc.want) {
				t.Fatalf("position missing or incorrect: %+v (ok=%v)", env, ok)
			}
			if !strings.Contains(env.Text, "https://www.openstreetmap.org/?mlat=") {
				t.Fatalf("map link missing: %s", env.Text)
			}
		})
	}
}

func TestBroadcastUpdatesDocument(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := router.New()
	ch := r.Subscribe()
	defer r.Unsubscribe(ch)
	updates := make(chan telegram.Update, 1)
	go broadcastUpdates(ctx, updates, r)

	var update telegram.Update
	if err := json.Unmarshal([]byte(`{"message":{"chat":{"id":42},"caption":"justificatif sport","document":{"file_id":"pdf-id","file_name":"Claire Levant.pdf","mime_type":"application/pdf","file_size":12345}}}`), &update); err != nil {
		t.Fatal(err)
	}
	updates <- update
	select {
	case raw := <-ch:
		var env map[string]any
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]any{
			"kind": "document", "text": "justificatif sport", "chat_id": float64(42),
			"file_id": "pdf-id", "file_name": "Claire Levant.pdf",
			"mime_type": "application/pdf", "file_size": float64(12345),
		} {
			if env[key] != want {
				t.Errorf("%s: want %v got %v", key, want, env[key])
			}
		}
	case <-time.After(time.Second):
		t.Fatal("PDF silently dropped instead of broadcast to bridge")
	}
}

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
