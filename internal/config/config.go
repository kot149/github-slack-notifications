package config

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// DefaultPath returns ./config.yml if it exists, otherwise
// $XDG_CONFIG_HOME/github-slack-notifications/config.yml (~/.config when unset).
func DefaultPath() string {
	if _, err := os.Stat("config.yml"); err == nil {
		return "config.yml"
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.yml"
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "github-slack-notifications", "config.yml")
}

// EnvPath returns the .env.local next to the config file at configPath.
func EnvPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), ".env.local")
}

// Parse reads the config file at path with defaults applied. A relative
// state_file is resolved against the config file's directory.
func Parse(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseBytes(path, b)
}

// ParseBytes is Parse with the file content given as b.
func ParseBytes(path string, b []byte) (*Config, error) {
	cfg := &Config{MarkAsRead: true, SortOldestFirst: true, Rollup: true, StateFile: "state.json"}
	cfg.Filter.OnlyUnread = true

	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !filepath.IsAbs(cfg.StateFile) {
		cfg.StateFile = filepath.Join(filepath.Dir(path), cfg.StateFile)
	}
	return cfg, nil
}

// Resolve is Parse plus the tokens and SLACK_CHANNEL from the environment or
// .env.local, without checking that they are set.
func Resolve(path string) (*Config, error) {
	cfg, err := Parse(path)
	if err != nil {
		return nil, err
	}
	if err := loadDotEnv(EnvPath(path)); err != nil {
		return nil, err
	}
	cfg.GitHubToken = os.Getenv("GITHUB_TOKEN")
	cfg.SlackToken = os.Getenv("SLACK_TOKEN")
	cfg.Slack.Channel = cmp.Or(os.Getenv("SLACK_CHANNEL"), cfg.Slack.Channel)
	return cfg, nil
}

// Load is Resolve, failing when a token or the Slack channel is missing.
func Load(path string) (*Config, error) {
	cfg, err := Resolve(path)
	if err != nil {
		return nil, err
	}
	if cfg.GitHubToken == "" || cfg.SlackToken == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN and SLACK_TOKEN must be set")
	}
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
		k, v, ok := ParseDotEnvLine(sc.Text())
		if !ok {
			continue
		}
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}

// ParseDotEnvLine parses a KEY=VALUE line of .env.local. ok is false for
// blank lines, comments and lines without "=".
func ParseDotEnvLine(line string) (k, v string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	k, v, ok = strings.Cut(strings.TrimPrefix(line, "export "), "=")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`), true
}
