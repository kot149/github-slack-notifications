package github

import (
	"testing"
	"time"
)

var base = time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func notif(reason string) Notification {
	n := Notification{Reason: reason, UpdatedAt: base}
	n.Subject.Type = "PullRequest"
	return n
}

func TestClassify(t *testing.T) {
	old := base.Add(-time.Hour)
	tests := []struct {
		name   string
		reason string
		facts  threadFacts
		want   string
	}{
		{"merged", "author", threadFacts{IsPR: true, CreatedAt: old, MergedAt: ptr(base)}, "Merged PR"},
		{"closed issue", "author", threadFacts{CreatedAt: old, ClosedAt: ptr(base)}, "Closed issue"},
		{"approved", "author", threadFacts{IsPR: true, CreatedAt: old, Review: &activity{Author: "alice", At: base, State: "APPROVED"}}, "Approved by alice"},
		{"changes requested", "author", threadFacts{IsPR: true, CreatedAt: old, Review: &activity{Author: "bob", At: base, State: "CHANGES_REQUESTED"}}, "Changes requested by bob"},
		{"approval beats comment", "author", threadFacts{IsPR: true, CreatedAt: old,
			Comment: &activity{Author: "bob", At: base},
			Review:  &activity{Author: "alice", At: base, State: "APPROVED"}}, "Approved by alice"},
		{"inline comment", "comment", threadFacts{IsPR: true, CreatedAt: old, Comment: &activity{Author: "carol", At: base, Inline: true}}, "New review comment by carol"},
		{"review comment reply", "comment", threadFacts{IsPR: true, CreatedAt: old, Comment: &activity{Author: "carol", At: base, Inline: true, Reply: true}}, "New reply to review comment by carol"},
		{"comment", "comment", threadFacts{IsPR: true, CreatedAt: old, Comment: &activity{Author: "dave", At: base}}, "New comment by dave"},
		{"commented review", "author", threadFacts{IsPR: true, CreatedAt: old, Review: &activity{Author: "erin", At: base, State: "COMMENTED"}}, "New review comment by erin"},
		{"stale activity ignored", "author", threadFacts{IsPR: true, CreatedAt: old, Comment: &activity{Author: "dave", At: old}}, "Updated PR"},
		{"review requested", "review_requested", threadFacts{IsPR: true, CreatedAt: base}, "Review requested"},
		{"mention", "mention", threadFacts{IsPR: true, CreatedAt: old}, "Mentioned in PR"},
		{"opened", "subscribed", threadFacts{IsPR: true, CreatedAt: base}, "Opened PR"},
		{"latest activity wins", "author", threadFacts{IsPR: true, CreatedAt: old, MergedAt: ptr(base.Add(-2 * time.Minute)), Comment: &activity{Author: "dave", At: base}}, "New comment by dave"},
		{"update after close is dropped", "comment", threadFacts{IsPR: true, CreatedAt: old, MergedAt: ptr(base.Add(-time.Hour))}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, got, _ := classify(notif(tt.reason), tt.facts); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifySkipsOwnActivity(t *testing.T) {
	old := base.Add(-time.Hour)
	tests := []struct {
		name  string
		facts threadFacts
		want  string
	}{
		{"own inline comment", threadFacts{IsPR: true, CreatedAt: old, Me: "me", Comment: &activity{Author: "Me", At: base, Inline: true}}, ""},
		{"own commented review", threadFacts{IsPR: true, CreatedAt: old, Me: "me", Review: &activity{Author: "me", At: base, State: "COMMENTED"}}, ""},
		{"own comment with other's approval", threadFacts{IsPR: true, CreatedAt: old, Me: "me",
			Comment: &activity{Author: "me", At: base},
			Review:  &activity{Author: "alice", At: base, State: "APPROVED"}}, "Approved by alice"},
		{"own approval with other's comment", threadFacts{IsPR: true, CreatedAt: old, Me: "me",
			Comment: &activity{Author: "dave", At: base},
			Review:  &activity{Author: "me", At: base, State: "APPROVED"}}, "New comment by dave"},
		{"other's comment", threadFacts{IsPR: true, CreatedAt: old, Me: "me", Comment: &activity{Author: "dave", At: base}}, "New comment by dave"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, got, _ := classify(notif("review_requested"), tt.facts); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyChanges(t *testing.T) {
	old := base.Add(-time.Hour)
	edit := func(kind, author string, at time.Time) change {
		return change{activity{Author: author, At: at, URL: "u"}, kind}
	}
	tests := []struct {
		name    string
		reason  string
		changes []change
		want    string
	}{
		{"description edit", "author", []change{edit(editedDescription, "alice", base)}, "Edited PR description by alice"},
		{"review comment edit", "author", []change{edit(editedReviewComment, "bot[bot]", base)}, "Edited review comment by bot[bot]"},
		{"latest change wins", "author", []change{
			edit(editedComment, "alice", base.Add(-2*time.Minute)),
			edit("HeadRefForcePushedEvent", "bob", base)}, "Force-pushed by bob"},
		{"own change is dropped", "author", []change{edit(editedDescription, "me", base)}, ""},
		{"stale change ignored", "author", []change{edit(editedComment, "alice", old)}, "Updated PR"},
		{"unknown kind ignored", "author", []change{edit("LabeledEvent", "alice", base)}, "Updated PR"},
		{"reason wins over change", "review_requested", []change{edit(editedComment, "alice", base)}, "Review requested"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := threadFacts{IsPR: true, CreatedAt: old, Me: "me", Changes: func() []change { return tt.changes }}
			if _, got, _ := classify(notif(tt.reason), f); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("not fetched when a comment explains it", func(t *testing.T) {
		f := threadFacts{IsPR: true, CreatedAt: old, Comment: &activity{Author: "dave", At: base},
			Changes: func() []change { t.Error("changes fetched"); return nil }}
		classify(notif("author"), f)
	})
}

func TestClassifyUsesLastReadAt(t *testing.T) {
	n := notif("comment")
	n.LastReadAt = ptr(base.Add(-90 * time.Second))
	merged := threadFacts{IsPR: true, CreatedAt: base.Add(-time.Hour), MergedAt: ptr(base.Add(-2 * time.Minute))}
	if _, got, _ := classify(n, merged); got != "" {
		t.Errorf("merge read before should not be reported again, got %q", got)
	}
	n.LastReadAt = ptr(base.Add(-10 * time.Minute))
	if _, got, _ := classify(n, merged); got != "Merged PR" {
		t.Errorf("merge after last read: got %q", got)
	}
}

func TestClassifyIgnoresReadAfterUpdate(t *testing.T) {
	n := notif("comment")
	n.LastReadAt = ptr(base.Add(time.Minute))
	f := threadFacts{IsPR: true, CreatedAt: base.Add(-time.Hour), Comment: &activity{Author: "dave", At: base}}
	if _, got, _ := classify(n, f); got != "New comment by dave" {
		t.Errorf("got %q", got)
	}
}

func TestClassifyLinksToActivity(t *testing.T) {
	old := base.Add(-time.Hour)
	comment := &activity{Author: "dave", At: base, URL: "https://github.com/o/r/pull/1#issuecomment-1"}
	review := &activity{Author: "alice", At: base, State: "APPROVED", URL: "https://github.com/o/r/pull/1#pullrequestreview-2"}
	tests := []struct {
		name  string
		facts threadFacts
		want  string
	}{
		{"comment", threadFacts{IsPR: true, CreatedAt: old, Comment: comment}, comment.URL},
		{"review", threadFacts{IsPR: true, CreatedAt: old, Review: review}, review.URL},
		{"merge", threadFacts{IsPR: true, CreatedAt: old, MergedAt: ptr(base)}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, got := classify(notif("author"), tt.facts); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
