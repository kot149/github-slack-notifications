package slack

import (
	"strings"
	"testing"

	"github.com/kot149/github-slack-notifications/internal/event"
)

func TestFormatEvent(t *testing.T) {
	e := event.Event{
		Emoji: ":red_circle:", Label: "CI failed (1/3)", LabelURL: "https://github.com/o/r/pull/1/checks",
		Repo:    event.Link{URL: "https://github.com/o/r", Text: "o/r"},
		Subject: event.Link{URL: "https://github.com/o/r/pull/1", Text: "#1 Fix <a> & <b>"},
		Details: []event.Link{{URL: "https://github.com/o/r/runs/9", Text: "lint"}},
	}
	want := "*<https://github.com/o/r/pull/1/checks|:red_circle: CI failed (1/3)>*\n" +
		"<https://github.com/o/r|o/r> <https://github.com/o/r/pull/1|#1 Fix &lt;a&gt; &amp; &lt;b&gt;>\n" +
		"• <https://github.com/o/r/runs/9|lint>"
	if got := format(e); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestMessagesSplitsLongRollups(t *testing.T) {
	e := event.Event{Label: strings.Repeat("x", 1500)}
	if got := len(Messages([]event.Event{e, e, e}, true)); got != 2 {
		t.Errorf("rollup: got %d messages, want 2", got)
	}
	if got := len(Messages([]event.Event{e, e, e}, false)); got != 3 {
		t.Errorf("no rollup: got %d messages, want 3", got)
	}
}
