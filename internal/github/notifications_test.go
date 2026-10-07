package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchNotificationsFollowsPages(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" {
			if got := r.Header.Get("If-Modified-Since"); got != "old" {
				t.Errorf("first page If-Modified-Since = %q", got)
			}
			w.Header().Set("X-Poll-Interval", "90")
			w.Header().Set("Date", "Mon, 02 Mar 2026 03:04:05 GMT")
			w.Header().Set("Last-Modified", "new")
			w.Header().Set("Link", fmt.Sprintf(`<%s/notifications?page=2>; rel="next"`, srv.URL))
			fmt.Fprint(w, `[{"id":"1"}]`)
			return
		}
		if got := r.Header.Get("If-Modified-Since"); got != "" {
			t.Errorf("later pages should not send If-Modified-Since, got %q", got)
		}
		fmt.Fprint(w, `[{"id":"2"}]`)
	}))
	defer srv.Close()
	g := New("token")
	g.BaseURL = srv.URL

	r, err := g.FetchNotifications(context.Background(), time.Now(), "old", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Notifications) != 2 || r.Notifications[1].ID != "2" {
		t.Errorf("notifications = %+v", r.Notifications)
	}
	if r.PollInterval != 90*time.Second || r.LastModified != "new" || !r.ServerTime.Equal(time.Date(2026, 3, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("result = %+v", r)
	}
}

func TestFetchNotificationsNotModified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	g := New("token")
	g.BaseURL = srv.URL

	r, err := g.FetchNotifications(context.Background(), time.Now(), "old", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !r.NotModified || r.LastModified != "old" || len(r.Notifications) != 0 {
		t.Errorf("result = %+v", r)
	}
}

func TestFetchNotificationsKeepsLastModifiedWhenOmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	g := New("token")
	g.BaseURL = srv.URL

	r, err := g.FetchNotifications(context.Background(), time.Now(), "old", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.LastModified != "old" {
		t.Errorf("LastModified = %q, want the previous value", r.LastModified)
	}
}
