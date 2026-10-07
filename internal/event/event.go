// Package event defines what gets forwarded, independent of where it comes from or is posted.
package event

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

	// SourceIDs identifies the notifications this event was built from.
	SourceIDs []string
}
