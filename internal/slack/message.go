package slack

import (
	"strings"

	"github.com/kot149/github-slack-notifications/internal/event"
)

// maxMessageLen keeps each post under Slack's recommended text length.
const maxMessageLen = 3500

var escaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// Escape makes s safe to put in message text.
func Escape(s string) string {
	return escaper.Replace(s)
}

func link(l event.Link) string {
	if l.URL == "" {
		return Escape(l.Text)
	}
	return "<" + l.URL + "|" + Escape(l.Text) + ">"
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

// Message is one Slack post and the notifications it covers.
type Message struct {
	Text      string
	SourceIDs []string
}

// Messages renders events into Slack messages: one per event, or rolled up and split to fit maxMessageLen.
func Messages(events []event.Event, rollup bool) []Message {
	var msgs []Message
	var cur Message
	for _, e := range events {
		s := format(e)
		switch {
		case !rollup:
			msgs = append(msgs, Message{Text: s, SourceIDs: e.SourceIDs})
			continue
		case cur.Text == "":
			cur.Text = s
		case len(cur.Text)+1+len(s) > maxMessageLen:
			msgs = append(msgs, cur)
			cur = Message{Text: s}
		default:
			cur.Text += "\n" + s
		}
		cur.SourceIDs = append(cur.SourceIDs, e.SourceIDs...)
	}
	if cur.Text != "" {
		msgs = append(msgs, cur)
	}
	return msgs
}
