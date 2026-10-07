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
