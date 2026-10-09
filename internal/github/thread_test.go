package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLatestReviewReadsLastPage(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s%s?page=3>; rel="last"`, srv.URL, r.URL.Path))
			fmt.Fprint(w, `[{"user":{"login":"old"},"state":"COMMENTED"}]`)
			return
		}
		fmt.Fprint(w, `[{"user":{"login":"a"},"state":"COMMENTED"},{"user":{"login":"b"},"state":"APPROVED","html_url":"https://github.com/o/r/pull/1#pullrequestreview-9"}]`)
	}))
	defer srv.Close()
	g := New("token")

	a, err := g.latestReview(context.Background(), srv.URL+"/repos/o/r/pulls/1")
	if err != nil {
		t.Fatal(err)
	}
	if a == nil || a.Author != "b" || a.State != "APPROVED" || a.URL == "" {
		t.Errorf("got %+v, want b's approval", a)
	}
}

func TestLatestReviewNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	a, err := New("token").latestReview(context.Background(), srv.URL+"/repos/o/r/pulls/1")
	if err != nil || a != nil {
		t.Errorf("got %+v, %v; want nil", a, err)
	}
}

func TestChanges(t *testing.T) {
	var vars map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		vars = body.Variables
		fmt.Fprint(w, `{"data":{"repository":{"issueOrPullRequest":{
			"url":"pr","lastEditedAt":"2026-01-01T00:00:00Z","editor":{"__typename":"User","login":"a"},
			"comments":{"nodes":[{"url":"c1","lastEditedAt":null,"editor":null}]},
			"reviews":{"nodes":[{"url":"r1","lastEditedAt":null,"editor":null,
				"comments":{"nodes":[{"url":"rc1","lastEditedAt":"2026-01-02T00:00:00Z","editor":{"__typename":"Bot","login":"rabbit"}}]}}]},
			"timelineItems":{"nodes":[{"__typename":"HeadRefForcePushedEvent","createdAt":"2026-01-03T00:00:00Z","actor":{"__typename":"User","login":"b"}}]}
		}}}}`)
	}))
	defer srv.Close()
	g := New("token")
	g.BaseURL = srv.URL
	n := Notification{UpdatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}
	n.Repository.FullName = "o/r"

	got, err := g.changes(context.Background(), n, 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []change{
		{activity{Author: "a", At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, editedDescription},
		{activity{Author: "rabbit[bot]", At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), URL: "rc1"}, editedReviewComment},
		{activity{Author: "b", At: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}, "HeadRefForcePushedEvent"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if vars["owner"] != "o" || vars["name"] != "r" || vars["number"] != float64(7) {
		t.Errorf("variables: %v", vars)
	}
}

func TestChangesGraphQLError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":null,"errors":[{"message":"boom"}]}`)
	}))
	defer srv.Close()
	g := New("token")
	g.BaseURL = srv.URL
	if _, err := g.changes(context.Background(), Notification{}, 1); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("got %v, want graphql error", err)
	}
}

func TestFetchActivityKinds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := ""
		if strings.HasSuffix(r.URL.Path, "/5") {
			reply = `,"in_reply_to_id":3`
		}
		fmt.Fprintf(w, `{"user":{"login":"a"},"html_url":"u","created_at":"2026-01-01T00:00:00Z","submitted_at":"2026-01-02T00:00:00Z","state":"APPROVED"%s}`, reply)
	}))
	defer srv.Close()
	g := New("token")

	tests := []struct {
		path   string
		state  string
		inline bool
		reply  bool
		day    int
	}{
		{"/repos/o/r/pulls/1/reviews/2", "APPROVED", false, false, 2},
		{"/repos/o/r/pulls/comments/3", "", true, false, 1},
		{"/repos/o/r/pulls/comments/5", "", true, true, 1},
		{"/repos/o/r/issues/comments/4", "", false, false, 1},
	}
	for _, tt := range tests {
		a, err := g.fetchActivity(context.Background(), srv.URL+tt.path)
		if err != nil {
			t.Fatal(err)
		}
		if a.State != tt.state || a.Inline != tt.inline || a.Reply != tt.reply || a.At.Day() != tt.day || a.Author != "a" {
			t.Errorf("%s: got %+v", tt.path, a)
		}
	}
}
