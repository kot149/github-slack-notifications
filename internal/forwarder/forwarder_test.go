package forwarder

import (
	"testing"

	"github.com/kot149/github-slack-notifications/internal/config"
	"github.com/kot149/github-slack-notifications/internal/github"
)

func TestKeep(t *testing.T) {
	f := &Forwarder{cfg: &config.Config{}}
	f.cfg.Filter.IncludeReasons = []string{"comment", "mention"}
	f.cfg.Filter.ExcludeRepositories = []string{"o/Skip"}

	n := github.Notification{Reason: "comment"}
	n.Repository.FullName = "o/r"
	if !f.keep(n) {
		t.Error("included reason should be kept")
	}
	n.Reason = "subscribed"
	if f.keep(n) {
		t.Error("reason outside include list should be dropped")
	}
	n.Reason = "comment"
	n.Repository.FullName = "o/skip"
	if f.keep(n) {
		t.Error("excluded repository should be dropped regardless of case")
	}
}
