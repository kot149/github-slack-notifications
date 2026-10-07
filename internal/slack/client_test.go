package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("token", Options{Channel: "C1", Username: "GitHub", IconURL: "https://example.com/i.png", IconEmoji: ":x:"})
	c.URL = srv.URL
	return c
}

func TestPostSendsPayload(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("Authorization = %q", got)
		}
		var p map[string]any
		json.NewDecoder(r.Body).Decode(&p)
		if p["channel"] != "C1" || p["text"] != "hi" || p["username"] != "GitHub" || p["icon_url"] == nil {
			t.Errorf("payload = %v", p)
		}
		if _, ok := p["icon_emoji"]; ok {
			t.Error("icon_emoji should be omitted when icon_url is set")
		}
		w.Write([]byte(`{"ok":true}`))
	})
	if err := c.Post(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
}

func TestPostRetriesRateLimit(t *testing.T) {
	var calls int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"ok":false,"error":"ratelimited"}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	})
	if err := c.Post(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestPostReportsSlackError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	})
	if err := c.Post(context.Background(), "hi"); err == nil || !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("want channel_not_found, got %v", err)
	}
}
