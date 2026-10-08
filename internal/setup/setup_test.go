package setup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yml")
	wrote, err := WriteConfig(path, []byte("a: 1\n"))
	if err != nil || !wrote {
		t.Fatalf("first write: wrote=%v err=%v", wrote, err)
	}
	wrote, err = WriteConfig(path, []byte("b: 2\n"))
	if err != nil || wrote {
		t.Fatalf("second write: wrote=%v err=%v", wrote, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "a: 1\n" {
		t.Errorf("existing file overwritten: %q", b)
	}
}

func TestUpdateDotEnv(t *testing.T) {
	in := "# comment\nexport GITHUB_TOKEN=\"old\"\nOTHER=x\nGITHUB_TOKEN=dup"
	got := string(UpdateDotEnv([]byte(in), map[string]string{"GITHUB_TOKEN": "new", "SLACK_CHANNEL": "C1"}))
	want := "# comment\nGITHUB_TOKEN=new\nOTHER=x\nSLACK_CHANNEL=C1\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	envPath := filepath.Join(dir, ".env.local")
	if err := os.WriteFile(envPath, []byte("SLACK_TOKEN=keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	in, err := os.CreateTemp(dir, "stdin")
	if err != nil {
		t.Fatal(err)
	}
	in.WriteString("ghp_x\n\n")
	in.Seek(0, 0)

	var out bytes.Buffer
	if err := Run(path, []byte("tmpl\n"), nil, in, &out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "tmpl\n" {
		t.Errorf("config = %q", b)
	}
	if b, _ := os.ReadFile(envPath); string(b) != "SLACK_TOKEN=keep\nGITHUB_TOKEN=ghp_x\n" {
		t.Errorf(".env.local = %q", b)
	}
	if fi, _ := os.Stat(envPath); fi.Mode().Perm() != 0o600 {
		t.Errorf(".env.local mode = %v", fi.Mode().Perm())
	}
	if !strings.Contains(out.String(), "Not set: SLACK_CHANNEL") {
		t.Errorf("output does not report SLACK_CHANNEL as missing:\n%s", out.String())
	}
}

func TestRunGiven(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	envPath := filepath.Join(dir, ".env.local")

	in, err := os.CreateTemp(dir, "stdin")
	if err != nil {
		t.Fatal(err)
	}
	in.WriteString("C9\n")
	in.Seek(0, 0)

	given := map[string]string{"GITHUB_TOKEN": "ghp_x", "SLACK_TOKEN": "xoxb-y"}
	var out bytes.Buffer
	if err := Run(path, []byte("tmpl\n"), given, in, &out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(envPath); string(b) != "GITHUB_TOKEN=ghp_x\nSLACK_TOKEN=xoxb-y\nSLACK_CHANNEL=C9\n" {
		t.Errorf(".env.local = %q", b)
	}
	if s := out.String(); strings.Contains(s, "GITHUB_TOKEN (") || !strings.Contains(s, "SLACK_CHANNEL (") {
		t.Errorf("prompts only for values not given:\n%s", s)
	}
}
