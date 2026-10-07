package github

import (
	"context"
	"log"
	"strings"

	"github.com/kot149/github-slack-notifications/internal/event"
)

// BuildEvent describes n, fetching the PR, issue, CI run or release it refers to.
func (g *Client) BuildEvent(ctx context.Context, n Notification) event.Event {
	e := event.Event{Repo: event.Link{URL: n.Repository.HTMLURL, Text: n.Repository.FullName}}
	var err error
	switch n.Subject.Type {
	case "PullRequest", "Issue":
		err = g.threadEvent(ctx, n, &e)
	case "CheckSuite":
		err = g.ciEvent(ctx, n, &e)
	case "Release":
		err = g.releaseEvent(ctx, n, &e)
	default:
		genericEvent(n, &e)
	}
	if err != nil {
		log.Printf("notification %s: %v; sending without details", n.ID, err)
		genericEvent(n, &e)
	}
	return e
}

func genericEvent(n Notification, e *event.Event) {
	u := n.Repository.HTMLURL
	if n.Subject.URL != "" {
		u = htmlURL(n.Subject.URL)
	}
	e.Emoji, e.Label, e.LabelURL = ":bell:", splitCamel(n.Subject.Type), u
	e.Subject = event.Link{URL: u, Text: n.Subject.Title}
	e.Details = nil
	e.GroupKey = ""
}

func (g *Client) releaseEvent(ctx context.Context, n Notification, e *event.Event) error {
	var r struct {
		HTMLURL string `json:"html_url"`
		TagName string `json:"tag_name"`
	}
	if err := g.get(ctx, n.Subject.URL, &r); err != nil {
		return err
	}
	e.Emoji, e.Label, e.LabelURL = ":rocket:", "Release", r.HTMLURL
	e.Subject = event.Link{URL: r.HTMLURL, Text: r.TagName}
	return nil
}

// splitCamel turns "RepositoryVulnerabilityAlert" into "Repository vulnerability alert".
func splitCamel(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}
