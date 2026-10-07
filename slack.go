package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxMessageLen keeps each post under Slack's recommended text length.
const maxMessageLen = 3500

var slackEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func slackLink(l Link) string {
	if l.URL == "" {
		return slackEscaper.Replace(l.Text)
	}
	return "<" + l.URL + "|" + slackEscaper.Replace(l.Text) + ">"
}

func formatEvent(e Event) string {
	lines := []string{
		"*" + slackLink(Link{e.LabelURL, e.Emoji + " " + e.Label}) + "*",
		slackLink(e.Repo) + " " + slackLink(e.Subject),
	}
	for _, d := range e.Details {
		lines = append(lines, "• "+slackLink(d))
	}
	return strings.Join(lines, "\n")
}

// buildMessages renders events into Slack messages: one per event, or rolled up and split to fit maxMessageLen.
func buildMessages(events []Event, rollup bool) []string {
	var msgs []string
	var cur string
	for _, e := range events {
		s := formatEvent(e)
		switch {
		case !rollup:
			msgs = append(msgs, s)
		case cur == "":
			cur = s
		case len(cur)+1+len(s) > maxMessageLen:
			msgs = append(msgs, cur)
			cur = s
		default:
			cur += "\n" + s
		}
	}
	if cur != "" {
		msgs = append(msgs, cur)
	}
	return msgs
}

type Slack struct {
	token string
	cfg   *Config
	http  *http.Client
}

func newSlack(cfg *Config) *Slack {
	return &Slack{token: cfg.SlackToken, cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

func (s *Slack) post(ctx context.Context, text string) error {
	payload := map[string]any{
		"channel":      s.cfg.Slack.Channel,
		"text":         text,
		"unfurl_links": false,
		"unfurl_media": false,
	}
	if s.cfg.Slack.Username != "" {
		payload["username"] = s.cfg.Slack.Username
	}
	if s.cfg.Slack.IconURL != "" {
		payload["icon_url"] = s.cfg.Slack.IconURL
	} else if s.cfg.Slack.IconEmoji != "" {
		payload["icon_emoji"] = s.cfg.Slack.IconEmoji
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/chat.postMessage", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+s.token)
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		res, err := s.http.Do(req)
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
