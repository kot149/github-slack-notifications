package config

import (
	"os"
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	cfg, err := ParseBytes("config.yml", []byte("filter:\n  include_repositories: [o/a, o/b]\nslack:\n  channel: C1\n"))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"filter.only_unread":          "true",
		"slack.channel":               "C1",
		"filter.include_repositories": "- o/a\n- o/b",
	} {
		if got, err := Get(cfg, key); err != nil || got != want {
			t.Errorf("Get(%q) = %q, %v; want %q", key, got, err, want)
		}
	}
	for _, key := range []string{"nope", "filter.nope", "slack.channel.x"} {
		if _, err := Get(cfg, key); err == nil {
			t.Errorf("Get(%q) succeeded, want error", key)
		}
	}
}

func TestSetKeepsLayout(t *testing.T) {
	src, err := os.ReadFile("../../config.yml")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ key, value, oldLine, newLine string }{
		{"rollup", "false", "rollup: true # one Slack", "rollup: false # one Slack"},
		{"filter.include_repositories", "[o/r, o/s]", "  include_repositories: [] # owner/repo", "  include_repositories: [o/r, o/s] # owner/repo"},
		{"slack.channel", "C0123", `  channel: "" # channel`, "  channel: C0123 # channel"},
		{"slack.username", "", "  username: GitHub", `  username: ""`},
	}
	for _, c := range cases {
		out, err := setBytes("config.yml", src, c.key, c.value)
		if err != nil {
			t.Fatalf("set %s: %v", c.key, err)
		}
		want := strings.Replace(string(src), c.oldLine, c.newLine, 1)
		if want == string(src) {
			t.Fatalf("test line %q not found in config.yml", c.oldLine)
		}
		if string(out) != want {
			t.Errorf("set %s=%s:\n%s", c.key, c.value, out)
		}
	}
}

func TestSetAddsKey(t *testing.T) {
	out, err := setBytes("config.yml", []byte("rollup: true\n"), "slack.channel", "C1")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseBytes("config.yml", out)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Slack.Channel != "C1" || !cfg.Rollup {
		t.Errorf("got %+v from\n%s", cfg, out)
	}
}

func TestSetRejectsInvalid(t *testing.T) {
	for _, c := range [][2]string{{"filter.nope", "1"}, {"rollup", "[a]"}, {"rollup.x", "1"}} {
		if _, err := setBytes("config.yml", []byte("rollup: true\n"), c[0], c[1]); err == nil {
			t.Errorf("set %s=%s succeeded, want error", c[0], c[1])
		}
	}
}
