package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kot149/github-slack-notifications/internal/event"
)

func TestCITitle(t *testing.T) {
	m := ciTitle.FindStringSubmatch("CI workflow run failed for feature/x branch")
	if m == nil || m[1] != "CI" || m[2] != "failed" || m[3] != "feature/x" {
		t.Errorf("got %q", m)
	}
}

func TestCIEventEscapesBranchAndFollowsPages(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls":
			if got := r.URL.Query().Get("head"); got != "o:fix/a&b" {
				t.Errorf("head = %q", got)
			}
			fmt.Fprint(w, `[{"html_url":"https://github.com/o/r/pull/1","number":1,"title":"T","head":{"sha":"abc"}}]`)
		case "/repos/o/r/commits/abc/check-runs":
			if r.URL.Query().Get("page") == "" {
				w.Header().Set("Link", fmt.Sprintf(`<%s%s?page=2>; rel="next"`, srv.URL, r.URL.Path))
				fmt.Fprint(w, `{"check_runs":[{"name":"build","status":"completed","conclusion":"success"}]}`)
				return
			}
			fmt.Fprint(w, `{"check_runs":[{"name":"lint","status":"completed","conclusion":"failure"}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}))
	defer srv.Close()

	g := New("token")
	g.BaseURL = srv.URL
	n := Notification{}
	n.Subject.Title = "CI workflow run failed for fix/a&b branch"
	n.Repository.FullName = "o/r"
	var e event.Event
	if err := g.ciEvent(context.Background(), n, &e); err != nil {
		t.Fatal(err)
	}
	if e.Label != "CI failed (1/2)" {
		t.Errorf("label = %q", e.Label)
	}
}
