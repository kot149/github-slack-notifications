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
	Reply  bool   // review comment replying to another in its thread
}

type threadFacts struct {
	IsPR      bool
	CreatedAt time.Time
	MergedAt  *time.Time
	ClosedAt  *time.Time
	Comment   *activity
	Review    *activity
	Me        string // login of the token owner, whose own activity isn't forwarded
	// Muted reports whether an event kind (event.Kinds) is muted. A muted activity is dropped like own activity.
	Muted func(kind string) bool
	// Changes lists edits and other events, fetched only when nothing else explains the notification.
	Changes func() []change
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
	muted := func(kind string) bool { return f.Muted != nil && f.Muted(kind) }
	// result returns emoji and label unless kind is muted.
	result := func(kind, emoji, label string) (string, string, string) {
		if muted(kind) {
			return "", "", ""
		}
		return emoji, label, ""
	}

	// Listed by priority, used when several activities happened at the same moment.
	var cands []candidate
	add := func(at *time.Time, kind, emoji, label, url string) {
		if at == nil || !isNew(n, *at) {
			return
		}
		if muted(kind) {
			emoji, label, url = "", "", ""
		}
		cands = append(cands, candidate{*at, emoji, label, url})
	}
	// Own activity takes part only to be dropped if it's the latest update.
	own := func(a *activity) bool { return f.Me != "" && strings.EqualFold(a.Author, f.Me) }
	addBy := func(a *activity, kind, emoji, label string) {
		if own(a) {
			add(&a.At, kind, "", "", "")
		} else {
			add(&a.At, kind, emoji, label+a.Author, a.URL)
		}
	}
	add(f.MergedAt, "merged", ":twisted_rightwards_arrows:", "Merged PR", "")
	add(f.ClosedAt, "closed", ":no_entry_sign:", "Closed "+noun, "")
	if r := f.Review; r != nil {
		switch r.State {
		case "APPROVED":
			addBy(r, "approved", ":white_check_mark:", "Approved by ")
		case "CHANGES_REQUESTED":
			addBy(r, "changes_requested", ":warning:", "Changes requested by ")
		}
	}
	if c := f.Comment; c != nil {
		if c.Reply {
			addBy(c, "review_reply", ":speech_balloon:", "New reply to review comment by ")
		} else if c.Inline {
			addBy(c, "review_comment", ":speech_balloon:", "New review comment by ")
		} else {
			addBy(c, "comment", ":speech_balloon:", "New comment by ")
		}
	}
	if r := f.Review; r != nil && r.State != "APPROVED" && r.State != "CHANGES_REQUESTED" {
		addBy(r, "review_comment", ":speech_balloon:", "New review comment by ")
	}

	// pick returns the highest-priority candidate among the latest ones, and false if there are none.
	pick := func() (emoji, label, url string, ok bool) {
		if len(cands) == 0 {
			return "", "", "", false
		}
		latest := cands[0].at
		for _, c := range cands {
			if c.at.After(latest) {
				latest = c.at
			}
		}
		for _, c := range cands {
			if c.label != "" && !c.at.Before(latest.Add(-sameMoment)) {
				return c.emoji, c.label, c.url, true
			}
		}
		return "", "", "", true
	}
	if emoji, label, url, ok := pick(); ok {
		return emoji, label, url
	}

	// pickChange is pick over edits and other changes. Call it at most once, since each call fetches them.
	pickChange := func() (emoji, label, url string, ok bool) {
		if f.Changes == nil {
			return "", "", "", false
		}
		for _, c := range f.Changes() {
			if kind, emoji, label := changeLabel(c.Kind, noun); label != "" {
				addBy(&c.activity, kind, emoji, label)
			}
		}
		return pick()
	}
	switch n.Reason {
	case "review_requested", "assign", "mention", "team_mention":
		// The reason sticks to the thread, so it also comes with later updates such as your own edits.
		if _, label, _, ok := pickChange(); ok && label == "" {
			return "", "", ""
		}
	}
	switch n.Reason {
	case "review_requested":
		return result("review_requested", ":eyes:", "Review requested")
	case "assign":
		return result("assigned", ":point_right:", "Assigned to "+noun)
	case "mention", "team_mention":
		return result("mentioned", ":mega:", "Mentioned in "+noun)
	}
	if f.MergedAt != nil || f.ClosedAt != nil {
		// e.g. the head branch was deleted after the merge that was already forwarded
		return "", "", ""
	}
	if isNew(n, f.CreatedAt) {
		return result("opened", ":sparkles:", "Opened "+noun)
	}
	if emoji, label, url, ok := pickChange(); ok {
		return emoji, label, url
	}
	return result("updated", ":arrows_counterclockwise:", "Updated "+noun)
}

// changeLabel returns the event kind, the emoji and the label, to be followed by the actor, for a change kind.
func changeLabel(change, noun string) (kind, emoji, label string) {
	switch change {
	case editedDescription:
		return "edited", ":pencil2:", "Edited " + noun + " description by "
	case editedComment:
		return "edited", ":pencil2:", "Edited comment by "
	case editedReview:
		return "edited", ":pencil2:", "Edited review by "
	case editedReviewComment:
		return "edited", ":pencil2:", "Edited review comment by "
	case "RenamedTitleEvent":
		return "renamed", ":pencil2:", "Renamed " + noun + " by "
	case "ReopenedEvent":
		return "reopened", ":recycle:", "Reopened " + noun + " by "
	case "ReadyForReviewEvent":
		return "ready_for_review", ":arrow_forward:", "Ready for review by "
	case "ConvertToDraftEvent":
		return "converted_to_draft", ":construction:", "Converted to draft by "
	case "HeadRefForcePushedEvent":
		return "force_pushed", ":arrow_up:", "Force-pushed by "
	case "BaseRefChangedEvent":
		return "base_changed", ":arrow_right_hook:", "Changed base branch by "
	case "ReviewDismissedEvent":
		return "review_dismissed", ":wastebasket:", "Review dismissed by "
	}
	return "", "", ""
}
