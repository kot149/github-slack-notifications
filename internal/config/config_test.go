package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func setTokens(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "gh")
	t.Setenv("SLACK_TOKEN", "sl")
	t.Setenv("SLACK_CHANNEL", "C1")
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	setTokens(t)
	_, err := Load(writeConfig(t, "filter:\n  include_repos: [o/r]\n"))
	if err == nil || !strings.Contains(err.Error(), "include_repos") {
		t.Errorf("want error naming the unknown key, got %v", err)
	}
}

func TestLoadBundledConfig(t *testing.T) {
	setTokens(t)
	if _, err := Load("../../config.yml"); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAcceptsEmptyFile(t *testing.T) {
	setTokens(t)
	cfg, err := Load(writeConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MarkAsRead || cfg.StateFile != "state.json" {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}
