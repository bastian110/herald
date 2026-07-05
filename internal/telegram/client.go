package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	ChatID    int64  `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

type sendRichMessageReq struct {
	ChatID      int64            `json:"chat_id"`
	RichMessage inputRichMessage `json:"rich_message"`
}

type inputRichMessage struct {
	HTML string `json:"html,omitempty"`
}

// maxMessageLen is the Telegram Bot API hard limit for sendMessage text.
const maxMessageLen = 4096

// SendMessage posts a text message to the given Telegram chat.
// HTML messages are sent via sendRichMessage so Bot API rich HTML blocks like
// tables render natively. If rich sending fails, it falls back to sendMessage
// with sanitized HTML for older Bot API deployments or malformed rich input.
// Non-HTML messages over Telegram's 4096-character sendMessage limit are split
// into consecutive messages.
func (c *Client) SendMessage(chatID int64, text string, parseMode string) error {
	if parseMode == "HTML" {
		if err := c.sendRichHTML(chatID, prepareTelegramRichHTML(text)); err == nil {
			return nil
		}
		text = prepareTelegramText(text, parseMode)
	} else {
		text = prepareTelegramText(text, parseMode)
	}

	chunks := splitMessage(text, maxMessageLen)
	for _, chunk := range chunks {
		if err := c.sendChunk(chatID, chunk, parseMode); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) sendRichHTML(chatID int64, htmlText string) error {
	url := fmt.Sprintf("%s/bot%s/sendRichMessage", c.baseURL, c.token)
	body, err := json.Marshal(sendRichMessageReq{
		ChatID:      chatID,
		RichMessage: inputRichMessage{HTML: htmlText},
	})
	if err != nil {
		return err
	}
	resp, err := c.httpCl.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return telegramHTTPError("sendRichMessage", resp)
	}
	return nil
}

func (c *Client) sendChunk(chatID int64, text string, parseMode string) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, c.token)
	body, err := json.Marshal(sendMessageReq{ChatID: chatID, Text: text, ParseMode: parseMode})
	if err != nil {
		return err
	}
	resp, err := c.httpCl.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return telegramHTTPError("sendMessage", resp)
	}
	return nil
}

func telegramHTTPError(method string, resp *http.Response) error {
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	responseBody = bytes.TrimSpace(responseBody)
	if len(responseBody) > 0 {
		return fmt.Errorf("telegram %s: HTTP %d: %s", method, resp.StatusCode, responseBody)
	}
	return fmt.Errorf("telegram %s: HTTP %d", method, resp.StatusCode)
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
