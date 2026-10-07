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

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	body := "# comment\n\nexport DOTENV_A=\"quoted\"\nDOTENV_B = 'single'\nDOTENV_C=keep\nnot a pair\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOTENV_C", "from env")
	for _, k := range []string{"DOTENV_A", "DOTENV_B"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"DOTENV_A": "quoted", "DOTENV_B": "single", "DOTENV_C": "from env"}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
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
