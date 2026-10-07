package forwarder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kot149/github-slack-notifications/internal/config"
	"github.com/kot149/github-slack-notifications/internal/state"
)

// fakeAPI serves the GitHub and Slack endpoints the forwarder uses.
type fakeAPI struct {
	t   *testing.T
	srv *httptest.Server

	mu            sync.Mutex
	notifications []map[string]any
	posts         []string
	marked        []string
	// failPostAt makes the n-th Slack post (1-based) fail with a server error.
	failPostAt int
	// notModified answers 304 to requests carrying If-Modified-Since.
	notModified bool
}

func newFakeAPI(t *testing.T) *fakeAPI {
	a := &fakeAPI{t: t}
	a.srv = httptest.NewServer(http.HandlerFunc(a.serve))
	t.Cleanup(a.srv.Close)
	return a
}

// addPR adds an unread "comment" notification for PR number on o/r.
func (a *fakeAPI) addPR(number int, updatedAt time.Time) {
	a.notifications = append(a.notifications, map[string]any{
		"id":         fmt.Sprint(number),
		"reason":     "comment",
		"unread":     true,
		"updated_at": updatedAt.UTC().Format(time.RFC3339),
		"subject": map[string]any{
			"title": fmt.Sprintf("PR %d", number),
			"url":   fmt.Sprintf("%s/repos/o/r/pulls/%d", a.srv.URL, number),
			"type":  "PullRequest",
		},
		"repository": map[string]any{"full_name": "o/r", "html_url": "https://github.com/o/r"},
	})
}

func (a *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))

	switch {
	case r.URL.Path == "/notifications":
		if a.notModified && r.Header.Get("If-Modified-Since") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		json.NewEncoder(w).Encode(a.notifications)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/notifications/threads/"):
		a.marked = append(a.marked, strings.TrimPrefix(r.URL.Path, "/notifications/threads/"))
		w.WriteHeader(http.StatusResetContent)
	case strings.HasSuffix(r.URL.Path, "/reviews"):
		w.Write([]byte("[]"))
	case strings.HasPrefix(r.URL.Path, "/repos/o/r/pulls/"):
		n := strings.TrimPrefix(r.URL.Path, "/repos/o/r/pulls/")
		json.NewEncoder(w).Encode(map[string]any{
			"html_url":   "https://github.com/o/r/pull/" + n,
			"number":     json.Number(n),
			"created_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		})
	case r.URL.Path == "/slack":
		var body struct{ Text string }
		json.NewDecoder(r.Body).Decode(&body)
		if a.failPostAt > 0 && len(a.posts)+1 == a.failPostAt {
			a.failPostAt = 0
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"ok":false,"error":"internal_error"}`))
			return
		}
		a.posts = append(a.posts, body.Text)
		w.Write([]byte(`{"ok":true}`))
	default:
		a.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotFound)
	}
}

func newTestForwarder(t *testing.T, a *fakeAPI, rollup bool) *Forwarder {
	cfg := &config.Config{MarkAsRead: true, SortOldestFirst: true, Rollup: rollup}
	cfg.StateFile = filepath.Join(t.TempDir(), "state.json")
	f := New(cfg, false, 0)
	f.gh.BaseURL = a.srv.URL
	f.slack.URL = a.srv.URL + "/slack"
	return f
}

func TestRunForwardsOnceAndSavesState(t *testing.T) {
	a := newFakeAPI(t)
	a.addPR(1, time.Now().Add(-time.Minute))
	f := newTestForwarder(t, a, true)

	for range 2 {
		if _, err := f.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.posts) != 1 || !strings.Contains(a.posts[0], "PR 1") {
		t.Errorf("posts = %q, want one post about PR 1", a.posts)
	}
	if len(a.marked) != 1 || a.marked[0] != "1" {
		t.Errorf("marked = %q, want [1]", a.marked)
	}
	st, err := state.Load(f.cfg.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if st.Since.IsZero() || st.LastModified == "" || len(st.Seen) != 1 {
		t.Errorf("state not saved: %+v", st)
	}
}

func TestRunNotModified(t *testing.T) {
	a := newFakeAPI(t)
	a.addPR(1, time.Now().Add(-time.Minute))
	f := newTestForwarder(t, a, true)
	if _, err := f.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	a.notModified = true
	a.addPR(2, time.Now())
	if _, err := f.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(a.posts) != 1 {
		t.Errorf("a 304 response should not post, got %d posts", len(a.posts))
	}
}

func TestRunDoesNotResendAfterPartialFailure(t *testing.T) {
	a := newFakeAPI(t)
	a.addPR(1, time.Now().Add(-2*time.Minute))
	a.addPR(2, time.Now().Add(-time.Minute))
	a.failPostAt = 2
	a.notModified = true
	f := newTestForwarder(t, a, false)

	if _, err := f.Run(context.Background()); err == nil {
		t.Fatal("want error from the failed post")
	}
	if _, err := f.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(a.posts) != 2 || !strings.Contains(a.posts[0], "PR 1") || !strings.Contains(a.posts[1], "PR 2") {
		t.Errorf("posts = %q, want PR 1 then PR 2 once each", a.posts)
	}
	if len(a.marked) != 2 {
		t.Errorf("marked = %q, want both", a.marked)
	}
}

func TestCheckStateFailsOnUnwritableStateFile(t *testing.T) {
	a := newFakeAPI(t)
	f := newTestForwarder(t, a, true)
	f.cfg.StateFile = filepath.Join(t.TempDir(), "missing-dir", "state.json")
	if err := f.CheckState(); err == nil {
		t.Error("want error for a state file that can't be written")
	}
}
