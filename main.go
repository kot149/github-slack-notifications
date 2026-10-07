package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kot149/github-slack-notifications/internal/config"
	"github.com/kot149/github-slack-notifications/internal/forwarder"
)

func main() {
	configPath := flag.String("config", "config.yml", "path to the config file")
	once := flag.Bool("once", false, "check once and exit instead of polling")
	dryRun := flag.Bool("dry-run", false, "print messages instead of posting; implies -once and changes nothing")
	lookback := flag.Duration("lookback", 0, "fetch notifications updated within this duration instead of since the last run, e.g. 24h")
	flag.Parse()
	log.SetFlags(log.LstdFlags)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	f := forwarder.New(cfg, *dryRun, *lookback)
	if !*dryRun {
		if err := f.CheckState(); err != nil {
			log.Fatal(err)
		}
	}
	if *once || *dryRun {
		if _, err := f.Run(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}

	for {
		wait, err := f.Run(ctx)
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
