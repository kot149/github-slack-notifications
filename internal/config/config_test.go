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
	if !cfg.MarkAsRead || filepath.Base(cfg.StateFile) != "state.json" {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}

func TestLoadResolvesFilesNextToConfig(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	os.Unsetenv("GITHUB_TOKEN")
	t.Setenv("SLACK_TOKEN", "sl")
	t.Setenv("SLACK_CHANNEL", "C1")
	path := writeConfig(t, "")
	dir := filepath.Dir(path)
	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("GITHUB_TOKEN=from-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHubToken != "from-file" {
		t.Errorf("GitHubToken = %q, want token from .env.local next to the config", cfg.GitHubToken)
	}
	if want := filepath.Join(dir, "state.json"); cfg.StateFile != want {
		t.Errorf("StateFile = %q, want %q", cfg.StateFile, want)
	}

	abs := filepath.Join(t.TempDir(), "s.json")
	cfg, err = Load(writeConfig(t, "state_file: "+abs+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateFile != abs {
		t.Errorf("StateFile = %q, want absolute path kept as %q", cfg.StateFile, abs)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got, want := DefaultPath(), filepath.Join("/xdg", "github-slack-notifications", "config.yml"); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}

	if err := os.WriteFile("config.yml", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DefaultPath(); got != "config.yml" {
		t.Errorf("DefaultPath() = %q, want config.yml in the working directory", got)
	}
}
