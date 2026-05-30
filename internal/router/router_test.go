package router_test

import (
	"testing"
	"time"

	"github.com/bastian110/herald/internal/router"
)

func TestBroadcastToAllSubscribers(t *testing.T) {
	r := router.New()
	ch1 := r.Subscribe()
	ch2 := r.Subscribe()
	defer r.Unsubscribe(ch1)
	defer r.Unsubscribe(ch2)

	msg := []byte(`{"op":"message","text":"hello"}` + "\n")
	r.Broadcast(msg)

	for i, ch := range []chan []byte{ch1, ch2} {
		select {
		case got := <-ch:
			if string(got) != string(msg) {
				t.Errorf("subscriber %d: want %q got %q", i, msg, got)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d: timeout waiting for broadcast", i)
		}
	}
}

func TestUnsubscribeRemovesChannel(t *testing.T) {
	r := router.New()
	ch := r.Subscribe()
	r.Unsubscribe(ch)
	// broadcasting to empty router must not panic
	r.Broadcast([]byte(`{"op":"message"}` + "\n"))
}

func TestBroadcastDropsWhenBufferFull(t *testing.T) {
	r := router.New()
	ch := r.Subscribe()
	defer r.Unsubscribe(ch)

	// fill the buffer (capacity 16) without reading
	for i := 0; i < 20; i++ {
		r.Broadcast([]byte(`{"op":"message"}` + "\n"))
	}
	// must not block or panic — excess messages are dropped
}
