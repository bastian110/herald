package client

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/bastian110/herald/internal/protocol"
)

// ErrTimeout is returned by Recv when no message arrives before the deadline.
var ErrTimeout = errors.New("herald: timed out waiting for message")

// Dial connects to the herald Unix socket.
func Dial(socketPath string) (net.Conn, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("herald: daemon not running at %s", socketPath)
	}
	return conn, nil
}

// Send writes a single send op and returns the daemon's ack/error envelope.
func Send(socketPath, text string, chatID int64) (protocol.Envelope, error) {
	conn, err := Dial(socketPath)
	if err != nil {
		return protocol.Envelope{}, err
	}
	defer conn.Close()

	out, err := protocol.Encode(protocol.Envelope{Op: "send", Text: text, ChatID: chatID})
	if err != nil {
		return protocol.Envelope{}, err
	}
	if _, err := conn.Write(out); err != nil {
		return protocol.Envelope{}, err
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return protocol.Envelope{}, errors.New("herald: no response from daemon")
	}
	return protocol.Decode(scanner.Bytes())
}

// Recv reads inbound message envelopes and calls handle for each. With follow
// false it returns after the first message. timeout==0 means no timeout.
func Recv(socketPath string, follow bool, timeout time.Duration, handle func(protocol.Envelope) error) error {
	conn, err := Dial(socketPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	if timeout > 0 {
		conn.SetReadDeadline(time.Now().Add(timeout))
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		env, err := protocol.Decode(scanner.Bytes())
		if err != nil {
			continue
		}
		if env.Op != "message" {
			continue
		}
		if err := handle(env); err != nil {
			return err
		}
		if !follow {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return ErrTimeout
		}
		return err
	}
	if !follow {
		return ErrTimeout
	}
	return nil
}
