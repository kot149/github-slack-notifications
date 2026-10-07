package github

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Notification struct {
	ID         string     `json:"id"`
	Reason     string     `json:"reason"`
	Unread     bool       `json:"unread"`
	UpdatedAt  time.Time  `json:"updated_at"`
	LastReadAt *time.Time `json:"last_read_at"`
	Subject    struct {
		Title            string `json:"title"`
		URL              string `json:"url"`
		LatestCommentURL string `json:"latest_comment_url"`
		Type             string `json:"type"`
	} `json:"subject"`
	Repository struct {
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
	} `json:"repository"`
}

type FetchResult struct {
	Notifications []Notification
	NotModified   bool
	LastModified  string
	ServerTime    time.Time
	PollInterval  time.Duration
}

// FetchNotifications lists notifications updated after since, following pagination.
// With lastModified set, an unchanged inbox returns NotModified without consuming rate limit.
func (g *Client) FetchNotifications(ctx context.Context, since time.Time, lastModified string, participating, all bool) (*FetchResult, error) {
	q := url.Values{}
	q.Set("since", since.UTC().Format(time.RFC3339))
	q.Set("participating", strconv.FormatBool(participating))
	q.Set("all", strconv.FormatBool(all))
	q.Set("per_page", "50")
	next := g.BaseURL + "/notifications?" + q.Encode()

	header := http.Header{}
	if lastModified != "" {
		header.Set("If-Modified-Since", lastModified)
	}

	r := &FetchResult{PollInterval: 60 * time.Second, ServerTime: time.Now()}
	for first := true; next != ""; first = false {
		var page []Notification
		res, err := g.do(ctx, http.MethodGet, next, header, &page)
		if err != nil {
			return nil, fmt.Errorf("fetch notifications (is the token scoped for notifications?): %w", err)
		}
		if first {
			if s, err := strconv.Atoi(res.Header.Get("X-Poll-Interval")); err == nil && s > 0 {
				r.PollInterval = time.Duration(s) * time.Second
			}
			if t, err := http.ParseTime(res.Header.Get("Date")); err == nil {
				r.ServerTime = t
			}
			if res.StatusCode == http.StatusNotModified {
				r.NotModified = true
				r.LastModified = lastModified
				return r, nil
			}
			// GitHub omits Last-Modified when the page is empty; the previous value still works for If-Modified-Since.
			r.LastModified = cmp.Or(res.Header.Get("Last-Modified"), lastModified)
			header = nil
		}
		r.Notifications = append(r.Notifications, page...)
		next = linkRel(res.Header.Get("Link"), "next")
	}
	return r, nil
}

func (g *Client) MarkAsRead(ctx context.Context, threadID string) error {
	_, err := g.do(ctx, http.MethodPatch, g.BaseURL+"/notifications/threads/"+threadID, nil, nil)
	return err
}
