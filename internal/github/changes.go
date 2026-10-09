package github

import (
	"context"
	"strings"
	"time"
)

// change is an update other than a new comment or review, e.g. an edit or a force-push.
type change struct {
	activity
	Kind string
}

const (
	editedDescription   = "edited_description"
	editedComment       = "edited_comment"
	editedReview        = "edited_review"
	editedReviewComment = "edited_review_comment"
)

const changesQuery = `query($owner: String!, $name: String!, $number: Int!, $since: DateTime!) {
  repository(owner: $owner, name: $name) {
    issueOrPullRequest(number: $number) {
      ... on Issue {
        ...edit
        comments(last: 30) { nodes { ...edit } }
        timelineItems(since: $since, last: 20, itemTypes: [REOPENED_EVENT, RENAMED_TITLE_EVENT]) { nodes { ...event } }
      }
      ... on PullRequest {
        ...edit
        comments(last: 30) { nodes { ...edit } }
        reviews(last: 20) { nodes { ...edit comments(last: 30) { nodes { ...edit } } } }
        timelineItems(since: $since, last: 20, itemTypes: [REOPENED_EVENT, RENAMED_TITLE_EVENT, READY_FOR_REVIEW_EVENT,
          CONVERT_TO_DRAFT_EVENT, HEAD_REF_FORCE_PUSHED_EVENT, BASE_REF_CHANGED_EVENT, REVIEW_DISMISSED_EVENT]) { nodes { ...event } }
      }
    }
  }
}
fragment edit on Comment {
  lastEditedAt editor { __typename login }
  ... on IssueComment { url }
  ... on PullRequestReview { url }
  ... on PullRequestReviewComment { url }
}
fragment event on Node {
  __typename
  ... on ReopenedEvent { createdAt actor { __typename login } }
  ... on RenamedTitleEvent { createdAt actor { __typename login } }
  ... on ReadyForReviewEvent { createdAt actor { __typename login } }
  ... on ConvertToDraftEvent { createdAt actor { __typename login } }
  ... on HeadRefForcePushedEvent { createdAt actor { __typename login } }
  ... on BaseRefChangedEvent { createdAt actor { __typename login } }
  ... on ReviewDismissedEvent { createdAt actor { __typename login } }
}`

type gqlActor struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
}

// name matches the REST API's login, which suffixes bots with [bot].
func (a *gqlActor) name() string {
	if a == nil {
		return ""
	}
	if a.Typename == "Bot" {
		return a.Login + "[bot]"
	}
	return a.Login
}

type gqlEdit struct {
	URL          string     `json:"url"`
	LastEditedAt *time.Time `json:"lastEditedAt"`
	Editor       *gqlActor  `json:"editor"`
}

// changes lists edits and timeline events of a PR or issue that may have happened since the user last read it.
func (g *Client) changes(ctx context.Context, n Notification, number int) ([]change, error) {
	owner, name, _ := strings.Cut(n.Repository.FullName, "/")
	since := n.UpdatedAt.Add(-eventWindow)
	if n.LastReadAt != nil && n.LastReadAt.Before(since) {
		since = *n.LastReadAt
	}
	type comments struct {
		Nodes []gqlEdit `json:"nodes"`
	}
	var res struct {
		Repository struct {
			Thread struct {
				gqlEdit
				Comments comments `json:"comments"`
				Reviews  struct {
					Nodes []struct {
						gqlEdit
						Comments comments `json:"comments"`
					} `json:"nodes"`
				} `json:"reviews"`
				TimelineItems struct {
					Nodes []struct {
						Typename  string    `json:"__typename"`
						CreatedAt time.Time `json:"createdAt"`
						Actor     *gqlActor `json:"actor"`
					} `json:"nodes"`
				} `json:"timelineItems"`
			} `json:"issueOrPullRequest"`
		} `json:"repository"`
	}
	vars := map[string]any{"owner": owner, "name": name, "number": number, "since": since.Format(time.RFC3339)}
	if err := g.graphql(ctx, changesQuery, vars, &res); err != nil {
		return nil, err
	}

	var out []change
	addEdit := func(e gqlEdit, kind string) {
		if e.LastEditedAt != nil {
			out = append(out, change{activity{Author: e.Editor.name(), At: *e.LastEditedAt, URL: e.URL}, kind})
		}
	}
	t := res.Repository.Thread
	addEdit(gqlEdit{LastEditedAt: t.LastEditedAt, Editor: t.Editor}, editedDescription)
	for _, c := range t.Comments.Nodes {
		addEdit(c, editedComment)
	}
	for _, r := range t.Reviews.Nodes {
		addEdit(r.gqlEdit, editedReview)
		for _, c := range r.Comments.Nodes {
			addEdit(c, editedReviewComment)
		}
	}
	for _, e := range t.TimelineItems.Nodes {
		out = append(out, change{activity{Author: e.Actor.name(), At: e.CreatedAt}, e.Typename})
	}
	return out, nil
}
