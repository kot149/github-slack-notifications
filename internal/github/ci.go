package github

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/kot149/github-slack-notifications/internal/event"
)

// CI notification titles look like "CI workflow run failed for main branch".
var ciTitle = regexp.MustCompile(`^(.+?) workflow run (\w+) for (.+) branch$`)

func (g *Client) ciEvent(ctx context.Context, n Notification, e *event.Event) error {
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
		e.Subject = event.Link{URL: actions, Text: branch}
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
	var failed []event.Link
	for _, r := range runs.CheckRuns {
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
