package github

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kot149/github-slack-notifications/internal/event"
)

type ghUser struct {
	Login string `json:"login"`
}

func (g *Client) threadEvent(ctx context.Context, n Notification, e *event.Event) error {
	var t struct {
		HTMLURL   string     `json:"html_url"`
		Number    int        `json:"number"`
		CreatedAt time.Time  `json:"created_at"`
		ClosedAt  *time.Time `json:"closed_at"`
		MergedAt  *time.Time `json:"merged_at"`
	}
	if err := g.get(ctx, n.Subject.URL, &t); err != nil {
		return err
	}
	f := threadFacts{IsPR: n.Subject.Type == "PullRequest", CreatedAt: t.CreatedAt, ClosedAt: t.ClosedAt, MergedAt: t.MergedAt}
	if f.MergedAt != nil {
		f.ClosedAt = nil
	}

	if c := n.Subject.LatestCommentURL; c != "" && c != n.Subject.URL {
		a, err := g.fetchActivity(ctx, c)
		if err != nil {
			log.Printf("notification %s: latest comment: %v", n.ID, err)
		} else if a.State != "" {
			f.Review = a
		} else {
			f.Comment = a
		}
	}
	if f.IsPR && f.Review == nil {
		r, err := g.latestReview(ctx, n.Subject.URL)
		if err != nil {
			log.Printf("notification %s: reviews: %v", n.ID, err)
		}
		f.Review = r
	}

	var url string
	e.Emoji, e.Label, url = classify(n, f)
	e.LabelURL = cmp.Or(url, t.HTMLURL)
	e.Subject = event.Link{URL: t.HTMLURL, Text: fmt.Sprintf("#%d %s", t.Number, n.Subject.Title)}
	return nil
}

func (g *Client) fetchActivity(ctx context.Context, apiURL string) (*activity, error) {
	var c struct {
		User        ghUser    `json:"user"`
		HTMLURL     string    `json:"html_url"`
		CreatedAt   time.Time `json:"created_at"`
		SubmittedAt time.Time `json:"submitted_at"`
		State       string    `json:"state"`
	}
	if err := g.get(ctx, apiURL, &c); err != nil {
		return nil, err
	}
	if strings.Contains(apiURL, "/reviews/") {
		return &activity{Author: c.User.Login, At: c.SubmittedAt, State: c.State, URL: c.HTMLURL}, nil
	}
	return &activity{Author: c.User.Login, At: c.CreatedAt, Inline: strings.Contains(apiURL, "/pulls/comments/"), URL: c.HTMLURL}, nil
}

func (g *Client) latestReview(ctx context.Context, pullURL string) (*activity, error) {
	type review struct {
		User        ghUser    `json:"user"`
		HTMLURL     string    `json:"html_url"`
		SubmittedAt time.Time `json:"submitted_at"`
		State       string    `json:"state"`
	}
	u := pullURL + "/reviews?per_page=100"
	var reviews []review
	res, err := g.do(ctx, "GET", u, nil, &reviews)
	if err != nil {
		return nil, err
	}
	if last := linkRel(res.Header.Get("Link"), "last"); last != "" {
		reviews = nil
		if err := g.get(ctx, last, &reviews); err != nil {
			return nil, err
		}
	}
	if len(reviews) == 0 {
		return nil, nil
	}
	r := reviews[len(reviews)-1]
	return &activity{Author: r.User.Login, At: r.SubmittedAt, State: r.State, URL: r.HTMLURL}, nil
}
