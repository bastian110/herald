package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client handles outbound Telegram API calls.
type Client struct {
	token   string
	baseURL string
	httpCl  *http.Client
}

// NewClient creates a Client targeting the real Telegram API.
func NewClient(token string) *Client {
	return NewClientWithBase(token, "https://api.telegram.org")
}

// NewClientWithBase creates a Client with a custom base URL (used in tests).
func NewClientWithBase(token, baseURL string) *Client {
	return &Client{
		token:   token,
		baseURL: baseURL,
		httpCl:  &http.Client{Timeout: 10 * time.Second},
	}
}

type sendMessageReq struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

// maxMessageLen is the Telegram Bot API hard limit for sendMessage text.
const maxMessageLen = 4096

// SendMessage posts a text message to the given Telegram chat.
// If text exceeds the Telegram 4096-character limit it is split into chunks
// and sent as consecutive messages.
func (c *Client) SendMessage(chatID int64, text string) error {
	chunks := splitMessage(text, maxMessageLen)
	for _, chunk := range chunks {
		if err := c.sendChunk(chatID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) sendChunk(chatID int64, text string) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, c.token)
	body, err := json.Marshal(sendMessageReq{ChatID: chatID, Text: text})
	if err != nil {
		return err
	}
	resp, err := c.httpCl.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram sendMessage: HTTP %d", resp.StatusCode)
	}
	return nil
}

// splitMessage breaks s into chunks of at most max runes, splitting on
// newline boundaries where possible to avoid cutting mid-sentence.
func splitMessage(s string, max int) []string {
	if len([]rune(s)) <= max {
		return []string{s}
	}
	var chunks []string
	runes := []rune(s)
	for len(runes) > 0 {
		if len(runes) <= max {
			chunks = append(chunks, string(runes))
			break
		}
		// Try to cut at the last newline within the max window.
		cut := max
		for i := max - 1; i > max/2; i-- {
			if runes[i] == '\n' {
				cut = i + 1
				break
			}
		}
		chunks = append(chunks, string(runes[:cut]))
		runes = runes[cut:]
	}
	return chunks
}
