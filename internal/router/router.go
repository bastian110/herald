package router

import "sync"

// Router broadcasts byte slices to all subscribed channels.
type Router struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func New() *Router {
	return &Router{clients: make(map[chan []byte]struct{})}
}

// Subscribe returns a new channel that will receive all future broadcasts.
// Buffer size 16: slow harnesses drop messages rather than blocking the daemon.
func (r *Router) Subscribe() chan []byte {
	ch := make(chan []byte, 16)
	r.mu.Lock()
	r.clients[ch] = struct{}{}
	r.mu.Unlock()
	return ch
}

// Unsubscribe removes ch from the broadcast set and closes it.
func (r *Router) Unsubscribe(ch chan []byte) {
	r.mu.Lock()
	delete(r.clients, ch)
	r.mu.Unlock()
	close(ch)
}

// Broadcast sends msg to every subscribed channel. Drops silently if a channel's buffer is full.
func (r *Router) Broadcast(msg []byte) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for ch := range r.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}
