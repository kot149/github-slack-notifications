package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kot149/github-slack-notifications/internal/event"
)

// CI notification titles look like "CI workflow run failed for main branch".
var ciTitle = regexp.MustCompile(`^(.+?) workflow run (\w+) for (.+) branch$`)

// closedPRWindow is how long after a PR is closed a CI run on its branch still counts as the PR's,
// e.g. a run that finishes after the PR is merged.
const closedPRWindow = time.Hour

func (g *Client) ciEvent(ctx context.Context, n Notification, e *event.Event) error {
	m := ciTitle.FindStringSubmatch(n.Subject.Title)
	if m == nil {
		return fmt.Errorf("unrecognized CI title %q", n.Subject.Title)
	}
	workflow, result, branch := m[1], m[2], m[3]
	repo := n.Repository.FullName
	owner, _, _ := strings.Cut(repo, "/")

	var pulls []struct {
		HTMLURL  string     `json:"html_url"`
		Number   int        `json:"number"`
		Title    string     `json:"title"`
		ClosedAt *time.Time `json:"closed_at"`
		Head     struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	q := url.Values{"state": {"all"}, "per_page": {"1"}, "head": {owner + ":" + branch}}
	if err := g.get(ctx, g.BaseURL+"/repos/"+repo+"/pulls?"+q.Encode(), &pulls); err != nil {
		return err
	}
	// A PR closed long before the run is from an earlier use of the branch, e.g. a past develop -> main
	// release PR, and its head SHA no longer matches the run.
	if len(pulls) > 0 && pulls[0].ClosedAt != nil && pulls[0].ClosedAt.Before(n.UpdatedAt.Add(-closedPRWindow)) {
		pulls = nil
	}
	if len(pulls) == 0 {
		actions := n.Repository.HTMLURL + "/actions"
		e.Emoji, e.Label, e.LabelURL = ciEmoji(result), fmt.Sprintf("CI %s: %s", result, workflow), actions
		e.Subject = event.Link{URL: actions, Text: branch}
		return nil
	}
	pr := pulls[0]

	type checkRun struct {
		Name       string `json:"name"`
		HTMLURL    string `json:"html_url"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	var checkRuns []checkRun
	for next := fmt.Sprintf("%s/repos/%s/commits/%s/check-runs?per_page=100", g.BaseURL, repo, pr.Head.SHA); next != ""; {
		var page struct {
			CheckRuns []checkRun `json:"check_runs"`
		}
		res, err := g.do(ctx, http.MethodGet, next, nil, &page)
		if err != nil {
			return err
		}
		checkRuns = append(checkRuns, page.CheckRuns...)
		next = linkRel(res.Header.Get("Link"), "next")
	}
	var total, pending int
	var failed []event.Link
	for _, r := range checkRuns {
		switch {
		case r.Status != "completed":
			pending++
		case r.Conclusion == "skipped" || r.Conclusion == "neutral":
			continue
		case r.Conclusion != "success":
			failed = append(failed, event.Link{URL: r.HTMLURL, Text: r.Name})
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
	e.Subject = event.Link{URL: pr.HTMLURL, Text: fmt.Sprintf("#%d %s", pr.Number, pr.Title)}
	e.GroupKey = "ci:" + pr.HTMLURL
	return nil
}

func ciEmoji(result string) string {
	if result == "succeeded" {
		return ":large_green_circle:"
	}
	return ":red_circle:"
}
