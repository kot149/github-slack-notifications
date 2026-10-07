package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

type Link struct {
	URL, Text string
}

// Event is one line item in a Slack message.
type Event struct {
	Emoji, Label string
	LabelURL     string
	Repo         Link
	Subject      Link
	Details      []Link
	UpdatedAt    time.Time

	// groupKey merges events that describe the same thing, e.g. several CI runs on one PR.
	groupKey string
}

// activity is something a person did on a PR or issue, used to tell which event a notification is about.
type activity struct {
	Author string
	At     time.Time
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

// classify picks the emoji and label that describe why a PR or issue notification was updated.
// An empty label means the update isn't worth forwarding.
func classify(n Notification, f threadFacts) (emoji, label string) {
	noun := "issue"
	if f.IsPR {
		noun = "PR"
	}

	type candidate struct {
		at           time.Time
		emoji, label string
	}
	// Listed by priority, used when several activities happened at the same moment.
	var cands []candidate
	add := func(at *time.Time, emoji, label string) {
		if at != nil && isNew(n, *at) {
			cands = append(cands, candidate{*at, emoji, label})
		}
	}
	add(f.MergedAt, ":twisted_rightwards_arrows:", "Merged PR")
	add(f.ClosedAt, ":no_entry_sign:", "Closed "+noun)
	if r := f.Review; r != nil {
		switch r.State {
		case "APPROVED":
			add(&r.At, ":white_check_mark:", "Approved by "+r.Author)
		case "CHANGES_REQUESTED":
			add(&r.At, ":warning:", "Changes requested by "+r.Author)
		}
	}
	if c := f.Comment; c != nil {
		if c.Inline {
			add(&c.At, ":speech_balloon:", "New review comment by "+c.Author)
		} else {
			add(&c.At, ":speech_balloon:", "New comment by "+c.Author)
		}
	}
	if r := f.Review; r != nil && r.State != "APPROVED" && r.State != "CHANGES_REQUESTED" {
		add(&r.At, ":speech_balloon:", "New review comment by "+r.Author)
	}

	if len(cands) > 0 {
		latest := cands[0].at
		for _, c := range cands {
			if c.at.After(latest) {
				latest = c.at
			}
		}
		for _, c := range cands {
			if !c.at.Before(latest.Add(-sameMoment)) {
				return c.emoji, c.label
			}
		}
	}

	switch n.Reason {
	case "review_requested":
		return ":eyes:", "Review requested"
	case "assign":
		return ":point_right:", "Assigned to " + noun
	case "mention", "team_mention":
		return ":mega:", "Mentioned in " + noun
	}
	if f.MergedAt != nil || f.ClosedAt != nil {
		// e.g. the head branch was deleted after the merge that was already forwarded
		return "", ""
	}
	if isNew(n, f.CreatedAt) {
		return ":sparkles:", "Opened " + noun
	}
	return ":arrows_counterclockwise:", "Updated " + noun
}

type ghUser struct {
	Login string `json:"login"`
}

func (g *GitHub) buildEvent(ctx context.Context, n Notification) Event {
	e := Event{
		Repo:      Link{n.Repository.HTMLURL, n.Repository.FullName},
		UpdatedAt: n.UpdatedAt,
	}
	var err error
	switch n.Subject.Type {
	case "PullRequest", "Issue":
		err = g.threadEvent(ctx, n, &e)
	case "CheckSuite":
		err = g.ciEvent(ctx, n, &e)
	case "Release":
		err = g.releaseEvent(ctx, n, &e)
	default:
		genericEvent(n, &e)
	}
	if err != nil {
		log.Printf("notification %s: %v; sending without details", n.ID, err)
		genericEvent(n, &e)
	}
	return e
}

func genericEvent(n Notification, e *Event) {
	u := n.Repository.HTMLURL
	if n.Subject.URL != "" {
		u = htmlURL(n.Subject.URL)
	}
	e.Emoji, e.Label, e.LabelURL = ":bell:", splitCamel(n.Subject.Type), u
	e.Subject = Link{u, n.Subject.Title}
	e.Details = nil
	e.groupKey = ""
}

func (g *GitHub) threadEvent(ctx context.Context, n Notification, e *Event) error {
	var t struct {
		HTMLURL   string     `json:"html_url"`
		Number    int        `json:"number"`
		CreatedAt time.Time  `json:"created_at"`
		ClosedAt  *time.Time `json:"closed_at"`
		MergedAt  *time.Time `json:"merged_at"`
	}
	if err := g.get(ctx, n.Subject.URL, &t); err != nil {
		return err
	}
	f := threadFacts{IsPR: n.Subject.Type == "PullRequest", CreatedAt: t.CreatedAt, ClosedAt: t.ClosedAt, MergedAt: t.MergedAt}
	if f.MergedAt != nil {
		f.ClosedAt = nil
	}

	if c := n.Subject.LatestCommentURL; c != "" && c != n.Subject.URL {
		a, err := g.fetchActivity(ctx, c)
		if err != nil {
			log.Printf("notification %s: latest comment: %v", n.ID, err)
		} else if a.State != "" {
			f.Review = a
		} else {
			f.Comment = a
		}
	}
	if f.IsPR && f.Review == nil {
		r, err := g.latestReview(ctx, n.Subject.URL)
		if err != nil {
			log.Printf("notification %s: reviews: %v", n.ID, err)
		}
		f.Review = r
	}

	e.Emoji, e.Label = classify(n, f)
	e.LabelURL = t.HTMLURL
	e.Subject = Link{t.HTMLURL, fmt.Sprintf("#%d %s", t.Number, n.Subject.Title)}
	return nil
}

func (g *GitHub) fetchActivity(ctx context.Context, apiURL string) (*activity, error) {
	var c struct {
		User        ghUser    `json:"user"`
		CreatedAt   time.Time `json:"created_at"`
		SubmittedAt time.Time `json:"submitted_at"`
		State       string    `json:"state"`
	}
	if err := g.get(ctx, apiURL, &c); err != nil {
		return nil, err
	}
	if strings.Contains(apiURL, "/reviews/") {
		return &activity{Author: c.User.Login, At: c.SubmittedAt, State: c.State}, nil
	}
	return &activity{Author: c.User.Login, At: c.CreatedAt, Inline: strings.Contains(apiURL, "/pulls/comments/")}, nil
}

func (g *GitHub) latestReview(ctx context.Context, pullURL string) (*activity, error) {
	type review struct {
		User        ghUser    `json:"user"`
		SubmittedAt time.Time `json:"submitted_at"`
		State       string    `json:"state"`
	}
	u := pullURL + "/reviews?per_page=100"
	var reviews []review
	res, err := g.do(ctx, "GET", u, nil, &reviews)
	if err != nil {
		return nil, err
	}
	if last := linkRel(res.Header.Get("Link"), "last"); last != "" {
		reviews = nil
		if err := g.get(ctx, last, &reviews); err != nil {
			return nil, err
		}
	}
	if len(reviews) == 0 {
		return nil, nil
	}
	r := reviews[len(reviews)-1]
	return &activity{Author: r.User.Login, At: r.SubmittedAt, State: r.State}, nil
}

func (g *GitHub) releaseEvent(ctx context.Context, n Notification, e *Event) error {
	var r struct {
		HTMLURL string `json:"html_url"`
		TagName string `json:"tag_name"`
	}
	if err := g.get(ctx, n.Subject.URL, &r); err != nil {
		return err
	}
	e.Emoji, e.Label, e.LabelURL = ":rocket:", "Release", r.HTMLURL
	e.Subject = Link{r.HTMLURL, r.TagName}
	return nil
}

// CI notification titles look like "CI workflow run failed for main branch".
var ciTitle = regexp.MustCompile(`^(.+?) workflow run (\w+) for (.+) branch$`)

func (g *GitHub) ciEvent(ctx context.Context, n Notification, e *Event) error {
	m := ciTitle.FindStringSubmatch(n.Subject.Title)
	if m == nil {
		return fmt.Errorf("unrecognized CI title %q", n.Subject.Title)
	}
	workflow, result, branch := m[1], m[2], m[3]
	repo := n.Repository.FullName
	owner, _, _ := strings.Cut(repo, "/")

	var pulls []struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Head    struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	q := fmt.Sprintf("%s/repos/%s/pulls?state=all&per_page=1&head=%s:%s", githubAPI, repo, owner, branch)
	if err := g.get(ctx, q, &pulls); err != nil {
		return err
	}
	if len(pulls) == 0 {
		actions := n.Repository.HTMLURL + "/actions"
		e.Emoji, e.Label, e.LabelURL = ciEmoji(result), fmt.Sprintf("CI %s: %s", result, workflow), actions
		e.Subject = Link{actions, branch}
		return nil
	}
	pr := pulls[0]

	var runs struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			HTMLURL    string `json:"html_url"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	if err := g.get(ctx, fmt.Sprintf("%s/repos/%s/commits/%s/check-runs?per_page=100", githubAPI, repo, pr.Head.SHA), &runs); err != nil {
		return err
	}
	var total, pending int
	var failed []Link
	for _, r := range runs.CheckRuns {
		switch {
		case r.Status != "completed":
			pending++
		case r.Conclusion == "skipped" || r.Conclusion == "neutral":
			continue
		case r.Conclusion != "success":
			failed = append(failed, Link{r.HTMLURL, r.Name})
		}
		total++
	}

	switch {
	case len(failed) > 0:
		e.Emoji, e.Label = ":red_circle:", fmt.Sprintf("CI failed (%d/%d)", len(failed), total)
		e.Details = failed
	case pending > 0:
		e.Emoji, e.Label = ":hourglass_flowing_sand:", fmt.Sprintf("CI running (%d/%d done)", total-pending, total)
	default:
		e.Emoji, e.Label = ":large_green_circle:", fmt.Sprintf("CI all green (%d)", total)
	}
	e.LabelURL = pr.HTMLURL + "/checks"
	e.Subject = Link{pr.HTMLURL, fmt.Sprintf("#%d %s", pr.Number, pr.Title)}
	e.groupKey = "ci:" + pr.HTMLURL
	return nil
}

func ciEmoji(result string) string {
	if result == "succeeded" {
		return ":large_green_circle:"
	}
	return ":red_circle:"
}

// splitCamel turns "RepositoryVulnerabilityAlert" into "Repository vulnerability alert".
func splitCamel(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}
