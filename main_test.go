package main

import (
	"strings"
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
			if _, got := classify(notif(tt.reason), tt.facts); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatEvent(t *testing.T) {
	e := Event{
		Emoji: ":red_circle:", Label: "CI failed (1/3)", LabelURL: "https://github.com/o/r/pull/1/checks",
		Repo:    Link{"https://github.com/o/r", "o/r"},
		Subject: Link{"https://github.com/o/r/pull/1", "#1 Fix <a> & <b>"},
		Details: []Link{{"https://github.com/o/r/runs/9", "lint"}},
	}
	want := "*<https://github.com/o/r/pull/1/checks|:red_circle: CI failed (1/3)>*\n" +
		"<https://github.com/o/r|o/r> <https://github.com/o/r/pull/1|#1 Fix &lt;a&gt; &amp; &lt;b&gt;>\n" +
		"• <https://github.com/o/r/runs/9|lint>"
	if got := formatEvent(e); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestBuildMessagesSplitsLongRollups(t *testing.T) {
	e := Event{Label: strings.Repeat("x", 1500)}
	if got := len(buildMessages([]Event{e, e, e}, true)); got != 2 {
		t.Errorf("rollup: got %d messages, want 2", got)
	}
	if got := len(buildMessages([]Event{e, e, e}, false)); got != 3 {
		t.Errorf("no rollup: got %d messages, want 3", got)
	}
}

func TestKeep(t *testing.T) {
	f := &Forwarder{cfg: &Config{}}
	f.cfg.Filter.IncludeReasons = []string{"comment", "mention"}
	f.cfg.Filter.ExcludeRepositories = []string{"o/Skip"}

	n := notif("comment")
	n.Repository.FullName = "o/r"
	if !f.keep(n) {
		t.Error("included reason should be kept")
	}
	n.Reason = "subscribed"
	if f.keep(n) {
		t.Error("reason outside include list should be dropped")
	}
	n.Reason = "comment"
	n.Repository.FullName = "o/skip"
	if f.keep(n) {
		t.Error("excluded repository should be dropped regardless of case")
	}
}

func TestLinkRel(t *testing.T) {
	h := `<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=5>; rel="last"`
	if got := linkRel(h, "next"); got != "https://api.github.com/x?page=2" {
		t.Errorf("next: %q", got)
	}
	if got := linkRel(h, "last"); got != "https://api.github.com/x?page=5" {
		t.Errorf("last: %q", got)
	}
	if got := linkRel("", "next"); got != "" {
		t.Errorf("empty: %q", got)
	}
}

func TestCITitle(t *testing.T) {
	m := ciTitle.FindStringSubmatch("CI workflow run failed for feature/x branch")
	if m == nil || m[1] != "CI" || m[2] != "failed" || m[3] != "feature/x" {
		t.Errorf("got %q", m)
	}
}

func TestClassifyUsesLastReadAt(t *testing.T) {
	n := notif("comment")
	n.LastReadAt = ptr(base.Add(-90 * time.Second))
	merged := threadFacts{IsPR: true, CreatedAt: base.Add(-time.Hour), MergedAt: ptr(base.Add(-2 * time.Minute))}
	if _, got := classify(n, merged); got != "" {
		t.Errorf("merge read before should not be reported again, got %q", got)
	}
	n.LastReadAt = ptr(base.Add(-10 * time.Minute))
	if _, got := classify(n, merged); got != "Merged PR" {
		t.Errorf("merge after last read: got %q", got)
	}
}

func TestClassifyIgnoresReadAfterUpdate(t *testing.T) {
	n := notif("comment")
	n.LastReadAt = ptr(base.Add(time.Minute))
	f := threadFacts{IsPR: true, CreatedAt: base.Add(-time.Hour), Comment: &activity{Author: "dave", At: base}}
	if _, got := classify(n, f); got != "New comment by dave" {
		t.Errorf("got %q", got)
	}
}
