package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	// overlap re-fetches a short window before the last fetch, because GitHub can deliver a
	// notification a little after its updated_at.
	overlap = 5 * time.Minute
	// firstLookback is how far back the very first run looks.
	firstLookback = 15 * time.Minute
)

func main() {
	configPath := flag.String("config", "config.yml", "path to the config file")
	once := flag.Bool("once", false, "check once and exit instead of polling")
	dryRun := flag.Bool("dry-run", false, "print messages instead of posting; implies -once and changes nothing")
	lookback := flag.Duration("lookback", 0, "fetch notifications updated within this duration instead of since the last run, e.g. 24h")
	flag.Parse()
	log.SetFlags(log.LstdFlags)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	f := &Forwarder{cfg: cfg, gh: newGitHub(cfg.GitHubToken), slack: newSlack(cfg), dryRun: *dryRun, lookback: *lookback}
	if *once || *dryRun {
		if _, err := f.run(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}

	for {
		wait, err := f.run(ctx)
		if err != nil {
			log.Print(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

type Forwarder struct {
	cfg      *Config
	gh       *GitHub
	slack    *Slack
	dryRun   bool
	lookback time.Duration
}

// run checks for new notifications once and returns how long to wait before the next check.
func (f *Forwarder) run(ctx context.Context) (time.Duration, error) {
	const retryWait = time.Minute

	st, err := loadState(f.cfg.StateFile)
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

	res, err := f.gh.fetchNotifications(ctx, since, lastModified, f.cfg.Filter.OnlyParticipating, !f.cfg.Filter.OnlyUnread)
	if err != nil {
		return retryWait, err
	}
	if res.NotModified {
		return res.PollInterval, nil
	}

	var targets []Notification
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
		for _, msg := range buildMessages(f.buildEvents(ctx, targets), f.cfg.Rollup) {
			os.Stdout.WriteString(msg + "\n\n")
		}
		return 0, nil
	}

	if len(targets) > 0 {
		events := f.buildEvents(ctx, targets)
		for _, msg := range buildMessages(events, f.cfg.Rollup) {
			if err := f.slack.post(ctx, msg); err != nil {
				return retryWait, err
			}
		}
		log.Printf("forwarded %d notifications", len(targets))

		if f.cfg.MarkAsRead {
			for _, n := range targets {
				if err := f.gh.markAsRead(ctx, n.ID); err != nil {
					log.Printf("mark %s as read: %v", n.ID, err)
				}
			}
		}
	}

	st = State{Since: res.ServerTime, LastModified: res.LastModified, Seen: seen}
	return res.PollInterval, saveState(f.cfg.StateFile, st)
}

func (f *Forwarder) keep(n Notification) bool {
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

// buildEvents turns notifications into events in display order, merging those with the same groupKey.
func (f *Forwarder) buildEvents(ctx context.Context, ns []Notification) []Event {
	slices.SortFunc(ns, func(a, b Notification) int {
		if f.cfg.SortOldestFirst {
			return a.UpdatedAt.Compare(b.UpdatedAt)
		}
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})

	var events []Event
	index := map[string]int{}
	for _, n := range ns {
		e := f.gh.buildEvent(ctx, n)
		if e.Label == "" {
			continue
		}
		if e.groupKey == "" {
			events = append(events, e)
			continue
		}
		// Keep the group at its latest position, since its checks reflect the newest run.
		if i, ok := index[e.groupKey]; ok && f.cfg.SortOldestFirst {
			events = slices.Delete(events, i, i+1)
			for k, v := range index {
				if v > i {
					index[k] = v - 1
				}
			}
		} else if ok {
			continue
		}
		index[e.groupKey] = len(events)
		events = append(events, e)
	}
	return events
}
