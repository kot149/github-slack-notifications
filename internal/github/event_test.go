package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildEventFallsBackWhenFetchFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	n := Notification{ID: "1"}
	n.Subject.Type = "Release"
	n.Subject.Title = "v1.0.0"
	n.Subject.URL = srv.URL + "/repos/o/r/releases/1"
	n.Repository.FullName = "o/r"
	n.Repository.HTMLURL = "https://github.com/o/r"
	e := New("token").BuildEvent(context.Background(), n)
	if e.Emoji != ":bell:" || e.Label != "Release" || e.Subject.Text != "v1.0.0" || e.Repo.Text != "o/r" {
		t.Errorf("got %+v, want the generic event", e)
	}
}

func TestSplitCamel(t *testing.T) {
	if got := splitCamel("RepositoryVulnerabilityAlert"); got != "Repository vulnerability alert" {
		t.Errorf("got %q", got)
	}
}
