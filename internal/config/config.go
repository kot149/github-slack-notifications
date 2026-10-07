package config

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	Filter struct {
		IncludeReasons      []string `yaml:"include_reasons"`
		ExcludeReasons      []string `yaml:"exclude_reasons"`
		IncludeRepositories []string `yaml:"include_repositories"`
		ExcludeRepositories []string `yaml:"exclude_repositories"`
		OnlyParticipating   bool     `yaml:"only_participating"`
		OnlyUnread          bool     `yaml:"only_unread"`
	} `yaml:"filter"`
	MarkAsRead      bool   `yaml:"mark_as_read"`
	SortOldestFirst bool   `yaml:"sort_oldest_first"`
	Rollup          bool   `yaml:"rollup"`
	StateFile       string `yaml:"state_file"`
	Slack           Slack  `yaml:"slack"`

	GitHubToken string `yaml:"-"`
	SlackToken  string `yaml:"-"`
}

type Slack struct {
	Channel   string `yaml:"channel"`
	Username  string `yaml:"username"`
	IconURL   string `yaml:"icon_url"`
	IconEmoji string `yaml:"icon_emoji"`
}

// Load reads the config file at path and the tokens from the environment or .env.local.
func Load(path string) (*Config, error) {
	cfg := &Config{MarkAsRead: true, SortOldestFirst: true, Rollup: true, StateFile: "state.json"}
	cfg.Filter.OnlyUnread = true

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if err := loadDotEnv(".env.local"); err != nil {
		return nil, err
	}
	cfg.GitHubToken = os.Getenv("GITHUB_TOKEN")
	cfg.SlackToken = os.Getenv("SLACK_TOKEN")
	if cfg.GitHubToken == "" || cfg.SlackToken == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN and SLACK_TOKEN must be set")
	}
	cfg.Slack.Channel = cmp.Or(os.Getenv("SLACK_CHANNEL"), cfg.Slack.Channel)
	if cfg.Slack.Channel == "" {
		return nil, fmt.Errorf("SLACK_CHANNEL or slack.channel in %s must be set", path)
	}
	return cfg, nil
}

// loadDotEnv sets KEY=VALUE pairs from path without overriding existing env vars.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}
