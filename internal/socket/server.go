package socket

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/bastian110/herald/internal/protocol"
	"github.com/bastian110/herald/internal/router"
)

// Server accepts Unix socket connections from harnesses.
type Server struct {
	path   string
	router *router.Router
	send   func(chatID int64, text string, parseMode string) error
}

// New creates a Server. send is called when a harness writes a "send" op.
func New(path string, r *router.Router, send func(int64, string, string) error) *Server {
	return &Server{path: path, router: r, send: send}
}

// Run listens on the Unix socket until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	os.Remove(s.path)
	l, err := net.Listen("unix", s.path)
	if err != nil {
		return fmt.Errorf("herald socket listen %s: %w", s.path, err)
	}
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("herald socket accept: %w", err)
			}
		}
		go s.handle(ctx, conn)
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	ch := s.router.Subscribe()
	defer s.router.Unsubscribe(ch)

	// Push inbound Telegram messages to this harness.
	go func() {
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				conn.Write(msg) //nolint:errcheck
			case <-ctx.Done():
				return
			}
		}
	}()

	// Read outbound send ops from this harness.
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // 4 MB max: handles long pi replies
	for scanner.Scan() {
		var env protocol.Envelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err != nil {
			continue
		}
		if env.Op != "send" {
			continue
		}
		err := s.send(env.ChatID, env.Text, env.ParseMode)
		var ack protocol.Envelope
		if err != nil {
			ack = protocol.Envelope{Op: "error", Error: err.Error()}
		} else {
			ack = protocol.Envelope{Op: "ack", OK: true}
		}
		b, _ := protocol.Encode(ack)
		conn.Write(b) //nolint:errcheck
	}
}
