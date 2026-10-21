package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
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
	branch := m[3]
	repo := n.Repository.FullName
	owner, _, _ := strings.Cut(repo, "/")

	var pulls []struct {
		HTMLURL  string     `json:"html_url"`
		Number   int        `json:"number"`
		Title    string     `json:"title"`
		User     ghUser     `json:"user"`
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
	// Only CI on your own PRs is forwarded; runs on other branches leave e.Label empty and are dropped.
	if len(pulls) == 0 {
		return nil
	}
	pr := pulls[0]
	me, err := g.viewer(ctx)
	if err != nil {
		return err
	}
	if pr.User.Login != me {
		return nil
	}

	type checkRun struct {
		Name       string `json:"name"`
		HTMLURL    string `json:"html_url"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		CheckSuite struct {
			ID int64 `json:"id"`
		} `json:"check_suite"`
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
	ignore := g.ignoredChecks(repo)
	var workflows map[int64]string
	if len(ignore) > 0 {
		if workflows, err = g.workflowNames(ctx, repo, pr.Head.SHA); err != nil {
			return err
		}
	}
	var total, pending int
	var failed []event.Link
	for _, r := range checkRuns {
		switch {
		case slices.ContainsFunc(ignore, func(s string) bool {
			return strings.EqualFold(s, r.Name) || strings.EqualFold(s, workflows[r.CheckSuite.ID])
		}):
			continue
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
		e.Status = "failed"
		g.mute(e, "ci_failed")
	case pending > 0:
		e.Emoji, e.Label = ":hourglass_flowing_sand:", fmt.Sprintf("CI running (%d/%d done)", total-pending, total)
		e.Status = "running"
		g.mute(e, "ci_running")
	default:
		e.Emoji, e.Label = ":large_green_circle:", fmt.Sprintf("CI all green (%d)", total)
		e.Status = "green"
		g.mute(e, "ci_all_green")
	}
	e.LabelURL = pr.HTMLURL + "/checks"
	e.Subject = event.Link{URL: pr.HTMLURL, Text: fmt.Sprintf("#%d %s", pr.Number, pr.Title)}
	e.GroupKey = "ci:" + pr.HTMLURL
	return nil
}

func (g *Client) ignoredChecks(repo string) []string {
	for r, names := range g.IgnoreChecks {
		if strings.EqualFold(r, repo) {
			return names
		}
	}
	return nil
}

// workflowNames maps the check suite IDs of the Actions runs on sha to their workflow names.
func (g *Client) workflowNames(ctx context.Context, repo, sha string) (map[int64]string, error) {
	names := map[int64]string{}
	for next := fmt.Sprintf("%s/repos/%s/actions/runs?head_sha=%s&per_page=100", g.BaseURL, repo, sha); next != ""; {
		var page struct {
			WorkflowRuns []struct {
				Name         string `json:"name"`
				CheckSuiteID int64  `json:"check_suite_id"`
			} `json:"workflow_runs"`
		}
		res, err := g.do(ctx, http.MethodGet, next, nil, &page)
		if err != nil {
			return nil, err
		}
		for _, r := range page.WorkflowRuns {
			names[r.CheckSuiteID] = r.Name
		}
		next = linkRel(res.Header.Get("Link"), "next")
	}
	return names, nil
}
