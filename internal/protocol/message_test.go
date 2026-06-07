package protocol_test

import (
	"testing"

	"github.com/bastian110/herald/internal/protocol"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	original := protocol.Envelope{
		Op:       "message",
		Kind:     "voice",
		Text:     "hello",
		From:     "bastian",
		ChatID:   123456,
		FileID:   "abc123",
		MimeType: "audio/ogg",
	}
	encoded, err := protocol.Encode(original)
	if err != nil {
		t.Fatal(err)
	}
	if encoded[len(encoded)-1] != '\n' {
		t.Error("encoded message must end with newline")
	}
	decoded, err := protocol.Decode(encoded[:len(encoded)-1])
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Op != original.Op {
		t.Errorf("Op: want %q got %q", original.Op, decoded.Op)
	}
	if decoded.Kind != original.Kind {
		t.Errorf("Kind: want %q got %q", original.Kind, decoded.Kind)
	}
	if decoded.Text != original.Text {
		t.Errorf("Text: want %q got %q", original.Text, decoded.Text)
	}
	if decoded.From != original.From {
		t.Errorf("From: want %q got %q", original.From, decoded.From)
	}
	if decoded.ChatID != original.ChatID {
		t.Errorf("ChatID: want %d got %d", original.ChatID, decoded.ChatID)
	}
	if decoded.FileID != original.FileID {
		t.Errorf("FileID: want %q got %q", original.FileID, decoded.FileID)
	}
	if decoded.MimeType != original.MimeType {
		t.Errorf("MimeType: want %q got %q", original.MimeType, decoded.MimeType)
	}
}

func TestEncodeAck(t *testing.T) {
	b, err := protocol.Encode(protocol.Envelope{Op: "ack", OK: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := protocol.Decode(b[:len(b)-1])
	if err != nil {
		t.Fatal(err)
	}
	if got.Op != "ack" || !got.OK {
		t.Errorf("want ack ok=true, got %+v", got)
	}
}
