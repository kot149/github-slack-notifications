package slack

import (
	"strings"

	"github.com/kot149/github-slack-notifications/internal/event"
)

// maxMessageLen keeps each post under Slack's recommended text length.
const maxMessageLen = 3500

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func link(l event.Link) string {
	if l.URL == "" {
		return escaper.Replace(l.Text)
	}
	return "<" + l.URL + "|" + escaper.Replace(l.Text) + ">"
}

func format(e event.Event) string {
	lines := []string{
		"*" + link(event.Link{URL: e.LabelURL, Text: e.Emoji + " " + e.Label}) + "*",
		link(e.Repo) + " " + link(e.Subject),
	}
	for _, d := range e.Details {
		lines = append(lines, "• "+link(d))
	}
	return strings.Join(lines, "\n")
}

// Messages renders events into Slack messages: one per event, or rolled up and split to fit maxMessageLen.
func Messages(events []event.Event, rollup bool) []string {
	var msgs []string
	var cur string
	for _, e := range events {
		s := format(e)
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
