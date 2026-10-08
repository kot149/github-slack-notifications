package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kot149/github-slack-notifications/internal/config"
	"github.com/kot149/github-slack-notifications/internal/forwarder"
	"github.com/kot149/github-slack-notifications/internal/setup"
)

//go:embed config.yml
var configTemplate []byte

func main() {
	log.SetFlags(log.LstdFlags)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			runInit(os.Args[2:])
			return
		case "config":
			runConfig(os.Args[2:])
			return
		}
	}

	configPath := flag.String("config", config.DefaultPath(), "path to the config file; .env.local and a relative state_file are read next to it")
	once := flag.Bool("once", false, "check once and exit instead of polling")
	dryRun := flag.Bool("dry-run", false, "print messages instead of posting; implies --once and changes nothing")
	lookback := flag.Duration("lookback", 0, "on the first check, fetch notifications updated within this duration instead of since the last run, e.g. 24h")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n  %[1]s [flags]\n  %[1]s init [--config path]   create the config file and .env.local\n  %[1]s config <command>       inspect or change the config (see config -h)\n\nFlags:\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

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

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	configPath := fs.String("config", config.DefaultPath(), "path of the config file to create; .env.local is written next to it")
	fs.Parse(args)
	if err := setup.Run(*configPath, configTemplate, os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
