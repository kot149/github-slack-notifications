package forwarder

import (
	"context"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/kot149/github-slack-notifications/internal/config"
	"github.com/kot149/github-slack-notifications/internal/event"
	"github.com/kot149/github-slack-notifications/internal/github"
	"github.com/kot149/github-slack-notifications/internal/slack"
	"github.com/kot149/github-slack-notifications/internal/state"
)

const (
	// overlap re-fetches a short window before the last fetch, because GitHub can deliver a
	// notification a little after its updated_at.
	overlap = 5 * time.Minute
	// firstLookback is how far back the very first run looks.
	firstLookback = 15 * time.Minute
)

type Forwarder struct {
	cfg      *config.Config
	gh       *github.Client
	slack    *slack.Client
	dryRun   bool
	lookback time.Duration
}

// New creates a Forwarder. With dryRun, messages are printed instead of posted and nothing is changed.
// A positive lookback fetches notifications updated within it instead of since the last run.
func New(cfg *config.Config, dryRun bool, lookback time.Duration) *Forwarder {
	return &Forwarder{
		cfg:      cfg,
		gh:       github.New(cfg.GitHubToken),
		slack:    slack.New(cfg.SlackToken, slack.Options(cfg.Slack)),
		dryRun:   dryRun,
		lookback: lookback,
	}
}

// Run checks for new notifications once and returns how long to wait before the next check.
func (f *Forwarder) Run(ctx context.Context) (time.Duration, error) {
	const retryWait = time.Minute

	st, err := state.Load(f.cfg.StateFile)
	if err != nil {
		return retryWait, err
	}
	since := st.Since.Add(-overlap)
	lastModified := st.LastModified
	if st.Since.IsZero() {
		since = time.Now().Add(-firstLookback)
		lastModified = ""
	}
	if f.lookback > 0 {
		since = time.Now().Add(-f.lookback)
		lastModified = ""
	}

	res, err := f.gh.FetchNotifications(ctx, since, lastModified, f.cfg.Filter.OnlyParticipating, !f.cfg.Filter.OnlyUnread)
	if err != nil {
		return retryWait, err
	}
	if res.NotModified {
		return res.PollInterval, nil
	}

	var targets []github.Notification
	seen := map[string]time.Time{}
	for _, n := range res.Notifications {
		seen[n.ID] = n.UpdatedAt
		if prev, ok := st.Seen[n.ID]; ok && !n.UpdatedAt.After(prev) {
			continue
		}
		if f.keep(n) {
			targets = append(targets, n)
		}
	}

	if f.dryRun {
		log.Printf("dry run: %d fetched since %s, %d after filters", len(res.Notifications), since.Format(time.RFC3339), len(targets))
		for _, msg := range slack.Messages(f.buildEvents(ctx, targets), f.cfg.Rollup) {
			os.Stdout.WriteString(msg + "\n\n")
		}
		return 0, nil
	}

	if len(targets) > 0 {
		events := f.buildEvents(ctx, targets)
		for _, msg := range slack.Messages(events, f.cfg.Rollup) {
			if err := f.slack.Post(ctx, msg); err != nil {
				return retryWait, err
			}
		}
		log.Printf("forwarded %d notifications", len(targets))

		if f.cfg.MarkAsRead {
			for _, n := range targets {
				if err := f.gh.MarkAsRead(ctx, n.ID); err != nil {
					log.Printf("mark %s as read: %v", n.ID, err)
				}
			}
		}
	}

	st = state.State{Since: res.ServerTime, LastModified: res.LastModified, Seen: seen}
	return res.PollInterval, state.Save(f.cfg.StateFile, st)
}

func (f *Forwarder) keep(n github.Notification) bool {
	flt := f.cfg.Filter
	repo := strings.ToLower(n.Repository.FullName)
	switch {
	case len(flt.IncludeReasons) > 0 && !slices.Contains(flt.IncludeReasons, n.Reason):
		return false
	case slices.Contains(flt.ExcludeReasons, n.Reason):
		return false
	case len(flt.IncludeRepositories) > 0 && !containsFold(flt.IncludeRepositories, repo):
		return false
	case containsFold(flt.ExcludeRepositories, repo):
		return false
	}
	return true
}

func containsFold(list []string, s string) bool {
	return slices.ContainsFunc(list, func(v string) bool { return strings.EqualFold(v, s) })
}

// buildEvents turns notifications into events in display order, merging those with the same GroupKey.
func (f *Forwarder) buildEvents(ctx context.Context, ns []github.Notification) []event.Event {
	slices.SortFunc(ns, func(a, b github.Notification) int {
		if f.cfg.SortOldestFirst {
			return a.UpdatedAt.Compare(b.UpdatedAt)
		}
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})

	var events []event.Event
	index := map[string]int{}
	for _, n := range ns {
		e := f.gh.BuildEvent(ctx, n)
		if e.Label == "" {
			continue
		}
		if e.GroupKey == "" {
			events = append(events, e)
			continue
		}
		// Keep the group at its latest position, since its checks reflect the newest run.
		if i, ok := index[e.GroupKey]; ok && f.cfg.SortOldestFirst {
			events = slices.Delete(events, i, i+1)
			for k, v := range index {
				if v > i {
					index[k] = v - 1
				}
			}
		} else if ok {
			continue
		}
		index[e.GroupKey] = len(events)
		events = append(events, e)
	}
	return events
}
