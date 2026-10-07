package forwarder

import (
	"context"
	"fmt"
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
	// alertAfter is how long fetching from GitHub must keep failing before it is reported to Slack,
	// e.g. when the token expired or lost its SSO authorization.
	alertAfter = 15 * time.Minute
)

type Forwarder struct {
	cfg      *config.Config
	gh       *github.Client
	slack    *slack.Client
	dryRun   bool
	lookback time.Duration

	// failingSince is when the current run of failed fetches began; zero while fetches succeed.
	failingSince time.Time
	alerted      bool
}

// New creates a Forwarder. With dryRun, messages are printed instead of posted and nothing is changed.
// A positive lookback makes the first successful run fetch notifications updated within it instead of since the last run.
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
	f.reportFetch(ctx, err)
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
			os.Stdout.WriteString(msg.Text + "\n\n")
		}
		return 0, nil
	}

	if len(targets) > 0 {
		updatedAt := map[string]time.Time{}
		for _, n := range targets {
			updatedAt[n.ID] = n.UpdatedAt
		}
		if st.Seen == nil {
			st.Seen = map[string]time.Time{}
		}
		posted := map[string]bool{}
		// A retry must refetch the unposted rest instead of getting 304 for an inbox that hasn't changed.
		st.LastModified = ""
		for _, msg := range slack.Messages(f.buildEvents(ctx, targets), f.cfg.Rollup) {
			if err := f.slack.Post(ctx, msg.Text); err != nil {
				return retryWait, err
			}
			// Record each post so that a failure later in this run doesn't resend it on retry.
			for _, id := range msg.SourceIDs {
				st.Seen[id] = updatedAt[id]
				posted[id] = true
			}
			if err := state.Save(f.cfg.StateFile, st); err != nil {
				return retryWait, err
			}
			f.markAsRead(ctx, msg.SourceIDs)
		}
		log.Printf("forwarded %d notifications", len(posted))

		// Notifications that produced no event, e.g. updates after an already forwarded merge.
		var dropped []string
		for _, n := range targets {
			if !posted[n.ID] {
				dropped = append(dropped, n.ID)
			}
		}
		f.markAsRead(ctx, dropped)
	}

	st = state.State{Since: res.ServerTime, LastModified: res.LastModified, Seen: seen}
	if err := state.Save(f.cfg.StateFile, st); err != nil {
		return retryWait, err
	}
	// Later polls continue from the saved state so they can get 304 instead of refetching the whole window.
	f.lookback = 0
	return res.PollInterval, nil
}

// reportFetch posts to Slack once fetching has failed for alertAfter, and again when it recovers,
// since a daemon that only logs the failure would stop forwarding unnoticed.
func (f *Forwarder) reportFetch(ctx context.Context, err error) {
	if f.dryRun {
		return
	}
	if err == nil {
		if f.alerted {
			f.alert(ctx, ":large_green_circle: Fetching GitHub notifications works again")
		}
		f.failingSince, f.alerted = time.Time{}, false
		return
	}
	if f.failingSince.IsZero() {
		f.failingSince = time.Now()
	}
	if !f.alerted && time.Since(f.failingSince) >= alertAfter {
		f.alerted = f.alert(ctx, fmt.Sprintf(":warning: Fetching GitHub notifications has failed since %s\n%s",
			f.failingSince.Format(time.DateTime), slack.Escape(err.Error())))
	}
}

func (f *Forwarder) alert(ctx context.Context, text string) bool {
	if err := f.slack.Post(ctx, text); err != nil {
		log.Printf("post alert: %v", err)
		return false
	}
	return true
}

func (f *Forwarder) markAsRead(ctx context.Context, ids []string) {
	if !f.cfg.MarkAsRead {
		return
	}
	for _, id := range ids {
		if err := f.gh.MarkAsRead(ctx, id); err != nil {
			log.Printf("mark %s as read: %v", id, err)
		}
	}
}

// CheckState fails if the state file can't be read and written, which would otherwise make every
// poll resend the same notifications.
func (f *Forwarder) CheckState() error {
	st, err := state.Load(f.cfg.StateFile)
	if err != nil {
		return err
	}
	return state.Save(f.cfg.StateFile, st)
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
		e.SourceIDs = []string{n.ID}
		if e.GroupKey == "" {
			events = append(events, e)
			continue
		}
		// Keep the group at its latest position, since its checks reflect the newest run.
		if i, ok := index[e.GroupKey]; ok && f.cfg.SortOldestFirst {
			e.SourceIDs = append(events[i].SourceIDs, e.SourceIDs...)
			events = slices.Delete(events, i, i+1)
			for k, v := range index {
				if v > i {
					index[k] = v - 1
				}
			}
		} else if ok {
			events[i].SourceIDs = append(events[i].SourceIDs, e.SourceIDs...)
			continue
		}
		index[e.GroupKey] = len(events)
		events = append(events, e)
	}
	return events
}
