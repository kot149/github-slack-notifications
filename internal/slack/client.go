package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Options sets where and as whom messages are posted.
type Options struct {
	Channel   string
	Username  string
	IconURL   string
	IconEmoji string
}

type Client struct {
	// URL is the chat.postMessage endpoint.
	URL   string
	token string
	opts  Options
	http  *http.Client
}

func New(token string, opts Options) *Client {
	return &Client{URL: "https://slack.com/api/chat.postMessage", token: token, opts: opts, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Post(ctx context.Context, text string) error {
	payload := map[string]any{
		"channel":      c.opts.Channel,
		"text":         text,
		"unfurl_links": false,
		"unfurl_media": false,
	}
	if c.opts.Username != "" {
		payload["username"] = c.opts.Username
	}
	if c.opts.IconURL != "" {
		payload["icon_url"] = c.opts.IconURL
	} else if c.opts.IconEmoji != "" {
		payload["icon_emoji"] = c.opts.IconEmoji
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		var r struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		err = json.NewDecoder(res.Body).Decode(&r)
		res.Body.Close()

		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			wait, _ := strconv.Atoi(res.Header.Get("Retry-After"))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(max(wait, 1)) * time.Second):
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("slack: %s: %w", res.Status, err)
		}
		if !r.OK {
			return fmt.Errorf("slack: %s", r.Error)
		}
		return nil
	}
}
