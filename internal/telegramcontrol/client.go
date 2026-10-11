// Package telegramcontrol is the desktop-only Telegram host boundary.
package telegramcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type Client struct {
	token        string
	base         string
	http         *http.Client
	sendMu       sync.Mutex
	lastSend     time.Time
	sendInterval time.Duration
}

func NewClient(token string) *Client {
	return &Client{sendInterval: 1100 * time.Millisecond, token: token, base: "https://api.telegram.org", http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Do deliberately hides URL-bearing network errors: the URL contains a secret.
func (c *Client) Do(ctx context.Context, method string, input, output any) error {
	if method == "sendMessage" {
		c.sendMu.Lock()
		delay := time.Until(c.lastSend.Add(c.sendInterval))
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				c.sendMu.Unlock()
				return ctx.Err()
			case <-timer.C:
			}
		}
		c.lastSend = time.Now()
		c.sendMu.Unlock()
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/bot"+c.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return errors.New("telegram request invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return errors.New("telegram connection failed")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return errors.New("telegram response unreadable")
	}
	var envelope struct {
		OK         bool            `json:"ok"`
		Result     json.RawMessage `json:"result"`
		Parameters struct {
			Retry int `json:"retry_after"`
		} `json:"parameters"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return errors.New("telegram response invalid")
	}
	if !envelope.OK {
		return fmt.Errorf("telegram refused request (HTTP %d; retry after %ds)", res.StatusCode, envelope.Parameters.Retry)
	}
	if output != nil {
		return json.Unmarshal(envelope.Result, output)
	}
	return nil
}

type User struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username"`
}
type Message struct {
	ID   int64  `json:"message_id"`
	From User   `json:"from"`
	Date int64  `json:"date"`
	Text string `json:"text"`
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	Reply   *Message        `json:"reply_to_message"`
	Forward json.RawMessage `json:"forward_origin"`
}
type Update struct {
	ID       int64    `json:"update_id"`
	Message  *Message `json:"message"`
	Callback *struct {
		ID      string   `json:"id"`
		From    User     `json:"from"`
		Message *Message `json:"message"`
		Data    string   `json:"data"`
	} `json:"callback_query"`
}
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}
