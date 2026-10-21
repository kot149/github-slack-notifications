// Package event defines what gets forwarded, independent of where it comes from or is posted.
package event

// Kinds lists the event kinds that mute.events can name.
var Kinds = []string{
	"merged", "closed", "reopened", "opened", "updated",
	"approved", "changes_requested", "review_comment", "review_reply", "review_dismissed", "comment",
	"review_requested", "assigned", "mentioned",
	"edited", "renamed", "ready_for_review", "converted_to_draft", "force_pushed", "base_changed",
	"ci_failed", "ci_running", "ci_all_green", "release", "other",
}

type Link struct {
	URL, Text string
}

// Event is one item in a message.
type Event struct {
	Emoji, Label string
	LabelURL     string
	Repo         Link
	Subject      Link
	Details      []Link

	// GroupKey merges events that describe the same thing, e.g. several CI runs on one PR.
	GroupKey string
	// Status summarizes the group's state, e.g. "failed" for CI, so unchanged repeats can be skipped.
	Status string

	// SourceIDs identifies the notifications this event was built from.
	SourceIDs []string
}
