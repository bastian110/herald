package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Update is a Telegram Bot API update object.
type Update struct {
	UpdateID int      `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

// Message is a Telegram message.
type Message struct {
	MessageID int    `json:"message_id"`
	Text      string `json:"text"`
	Chat      Chat   `json:"chat"`
	From      *User  `json:"from,omitempty"`
}

// Chat holds the chat ID.
type Chat struct {
	ID int64 `json:"id"`
}

// User holds sender info.
type User struct {
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// Poller long-polls the Telegram getUpdates endpoint and emits updates to a channel.
type Poller struct {
	client  *Client
	timeout int // Telegram long-poll timeout in seconds
	offset  int
	httpCl  *http.Client
}

// NewPoller creates a Poller. timeout is the Telegram server-side long-poll duration in seconds.
func NewPoller(client *Client, timeout int) *Poller {
	return &Poller{
		client:  client,
		timeout: timeout,
		// HTTP client timeout must exceed the Telegram long-poll timeout.
		httpCl: &http.Client{Timeout: time.Duration(timeout+5) * time.Second},
	}
}

func (p *Poller) getUpdates(ctx context.Context) ([]Update, error) {
	url := fmt.Sprintf(
		"%s/bot%s/getUpdates?timeout=%d&offset=%d",
		p.client.baseURL, p.client.token, p.timeout, p.offset,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.httpCl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Result) > 0 {
		p.offset = result.Result[len(result.Result)-1].UpdateID + 1
	}
	return result.Result, nil
}

// Run polls Telegram in a loop until ctx is cancelled, sending updates to the channel.
// On error, it retries with exponential backoff (1s → 2s → … → 60s cap).
func (p *Poller) Run(ctx context.Context, updates chan<- Update) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		batch, err := p.getUpdates(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				if backoff < 60*time.Second {
					backoff *= 2
				}
				continue
			}
		}
		backoff = time.Second
		for _, u := range batch {
			select {
			case updates <- u:
			case <-ctx.Done():
				return
			}
		}
	}
}
