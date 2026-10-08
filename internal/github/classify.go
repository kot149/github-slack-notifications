package github

import (
	"strings"
	"time"
)

// activity is something a person did on a PR or issue, used to tell which event a notification is about.
type activity struct {
	Author string
	At     time.Time
	URL    string // github.com page of the comment or review
	State  string // review state; empty for comments
	Inline bool   // review comment on a diff line
}

type threadFacts struct {
	IsPR      bool
	CreatedAt time.Time
	MergedAt  *time.Time
	ClosedAt  *time.Time
	Comment   *activity
	Review    *activity
	Me        string // login of the token owner, whose own activity isn't forwarded
}

// eventWindow is how close an activity must be to the notification's updated_at to be its cause.
const eventWindow = 3 * time.Minute

// sameMoment is how close two activities must be to count as one action, e.g. approving and commenting.
const sameMoment = time.Minute

// isNew reports whether t happened since the user last read the thread, or around updated_at
// if the thread was never read or was read after its latest update.
func isNew(n Notification, t time.Time) bool {
	if t.After(n.UpdatedAt.Add(eventWindow)) {
		return false
	}
	if n.LastReadAt != nil && n.LastReadAt.Before(n.UpdatedAt) {
		return t.After(*n.LastReadAt)
	}
	return t.After(n.UpdatedAt.Add(-eventWindow))
}

// classify picks the emoji and label that describe why a PR or issue notification was updated,
// and the page of the comment or review behind it, if any. An empty label means the update isn't worth forwarding.
func classify(n Notification, f threadFacts) (emoji, label, url string) {
	noun := "issue"
	if f.IsPR {
		noun = "PR"
	}

	type candidate struct {
		at                time.Time
		emoji, label, url string
	}
	// Listed by priority, used when several activities happened at the same moment.
	var cands []candidate
	add := func(at *time.Time, emoji, label, url string) {
		if at != nil && isNew(n, *at) {
			cands = append(cands, candidate{*at, emoji, label, url})
		}
	}
	// Own activity takes part only to be dropped if it's the latest update.
	own := func(a *activity) bool { return f.Me != "" && strings.EqualFold(a.Author, f.Me) }
	addBy := func(a *activity, emoji, label string) {
		if own(a) {
			add(&a.At, "", "", "")
		} else {
			add(&a.At, emoji, label+a.Author, a.URL)
		}
	}
	add(f.MergedAt, ":twisted_rightwards_arrows:", "Merged PR", "")
	add(f.ClosedAt, ":no_entry_sign:", "Closed "+noun, "")
	if r := f.Review; r != nil {
		switch r.State {
		case "APPROVED":
			addBy(r, ":white_check_mark:", "Approved by ")
		case "CHANGES_REQUESTED":
			addBy(r, ":warning:", "Changes requested by ")
		}
	}
	if c := f.Comment; c != nil {
		if c.Inline {
			addBy(c, ":speech_balloon:", "New review comment by ")
		} else {
			addBy(c, ":speech_balloon:", "New comment by ")
		}
	}
	if r := f.Review; r != nil && r.State != "APPROVED" && r.State != "CHANGES_REQUESTED" {
		addBy(r, ":speech_balloon:", "New review comment by ")
	}

	if len(cands) > 0 {
		latest := cands[0].at
		for _, c := range cands {
			if c.at.After(latest) {
				latest = c.at
			}
		}
		for _, c := range cands {
			if c.label != "" && !c.at.Before(latest.Add(-sameMoment)) {
				return c.emoji, c.label, c.url
			}
		}
		return "", "", ""
	}

	switch n.Reason {
	case "review_requested":
		return ":eyes:", "Review requested", ""
	case "assign":
		return ":point_right:", "Assigned to " + noun, ""
	case "mention", "team_mention":
		return ":mega:", "Mentioned in " + noun, ""
	}
	if f.MergedAt != nil || f.ClosedAt != nil {
		// e.g. the head branch was deleted after the merge that was already forwarded
		return "", "", ""
	}
	if isNew(n, f.CreatedAt) {
		return ":sparkles:", "Opened " + noun, ""
	}
	return ":arrows_counterclockwise:", "Updated " + noun, ""
}
