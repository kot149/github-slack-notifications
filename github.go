package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const githubAPI = "https://api.github.com"

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

type GitHub struct {
	token string
	http  *http.Client
}

var errNotFound = errors.New("not found")

func newGitHub(token string) *GitHub {
	return &GitHub{token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

func (g *GitHub) do(ctx context.Context, method, rawURL string, header http.Header, out any) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := g.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == http.StatusNotModified:
		return res, nil
	case res.StatusCode == http.StatusNotFound:
		return res, fmt.Errorf("%s %s: %w", method, rawURL, errNotFound)
	case res.StatusCode >= 300:
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return res, fmt.Errorf("%s %s: %s: %s", method, rawURL, res.Status, strings.TrimSpace(string(body)))
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return res, fmt.Errorf("%s %s: decode: %w", method, rawURL, err)
		}
	}
	return res, nil
}

func (g *GitHub) get(ctx context.Context, rawURL string, out any) error {
	_, err := g.do(ctx, http.MethodGet, rawURL, nil, out)
	return err
}

type FetchResult struct {
	Notifications []Notification
	NotModified   bool
	LastModified  string
	ServerTime    time.Time
	PollInterval  time.Duration
}

// fetchNotifications lists notifications updated after since, following pagination.
// With lastModified set, an unchanged inbox returns NotModified without consuming rate limit.
func (g *GitHub) fetchNotifications(ctx context.Context, since time.Time, lastModified string, participating, all bool) (*FetchResult, error) {
	q := url.Values{}
	q.Set("since", since.UTC().Format(time.RFC3339))
	q.Set("participating", strconv.FormatBool(participating))
	q.Set("all", strconv.FormatBool(all))
	q.Set("per_page", "50")
	next := githubAPI + "/notifications?" + q.Encode()

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

func (g *GitHub) markAsRead(ctx context.Context, threadID string) error {
	_, err := g.do(ctx, http.MethodPatch, githubAPI+"/notifications/threads/"+threadID, nil, nil)
	return err
}

// linkRel extracts the URL for rel from an RFC 8288 Link header.
func linkRel(link, rel string) string {
	for part := range strings.SplitSeq(link, ",") {
		u, params, ok := strings.Cut(part, ";")
		if ok && strings.Contains(params, `rel="`+rel+`"`) {
			return strings.Trim(strings.TrimSpace(u), "<>")
		}
	}
	return ""
}

// htmlURL converts an API URL to its github.com page as a fallback when the API object can't be fetched.
func htmlURL(apiURL string) string {
	u := strings.Replace(apiURL, "https://api.github.com/repos/", "https://github.com/", 1)
	return strings.Replace(u, "/pulls/", "/pull/", 1)
}
