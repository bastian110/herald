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

// SendMessage posts a text message to the given Telegram chat.
func (c *Client) SendMessage(chatID int64, text string) error {
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
