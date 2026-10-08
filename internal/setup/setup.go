// Package setup implements the init command: it places the config file and
// prompts for the values stored in .env.local.
package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/kot149/github-slack-notifications/internal/config"
)

type secret struct {
	key, prompt string
	hidden      bool
}

var secrets = []secret{
	{"GITHUB_TOKEN", "GITHUB_TOKEN (classic PAT with the notifications scope)", true},
	{"SLACK_TOKEN", "SLACK_TOKEN (bot token, xoxb-...)", true},
	{"SLACK_CHANNEL", "SLACK_CHANNEL (channel or user ID)", false},
}

// Run writes template to configPath unless it exists, then asks for each
// secret not in given and saves the values to .env.local next to the config
// file. given is keyed by the .env.local key, e.g. GITHUB_TOKEN.
func Run(configPath string, template []byte, given map[string]string, in *os.File, out io.Writer) error {
	wrote, err := WriteConfig(configPath, template)
	if err != nil {
		return err
	}
	if wrote {
		fmt.Fprintf(out, "Created %s\n", configPath)
	} else {
		fmt.Fprintf(out, "%s already exists, leaving it as is\n", configPath)
	}

	envPath := config.EnvPath(configPath)
	content, err := os.ReadFile(envPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	current := ParseDotEnv(content)

	values := map[string]string{}
	var ask []secret
	for _, s := range secrets {
		if v := strings.TrimSpace(given[s.key]); v != "" {
			values[s.key] = v
		} else {
			ask = append(ask, s)
		}
	}
	if len(ask) > 0 {
		fmt.Fprintf(out, "\nValues for %s (Enter keeps the current value):\n", envPath)
	}
	r := bufio.NewReader(in)
	var missing []string
	for _, s := range ask {
		label := s.prompt
		if current[s.key] != "" {
			label += " [set]"
		}
		fmt.Fprintf(out, "%s: ", label)
		v, err := readLine(r, in, s.hidden, out)
		if err != nil {
			return err
		}
		switch {
		case v != "":
			values[s.key] = v
		case current[s.key] == "":
			missing = append(missing, s.key)
		}
	}

	if len(values) > 0 {
		if err := os.WriteFile(envPath, UpdateDotEnv(content, values), 0o600); err != nil {
			return err
		}
		// WriteFile keeps the mode of an existing file.
		if err := os.Chmod(envPath, 0o600); err != nil {
			return err
		}
		fmt.Fprintf(out, "Saved %s\n", envPath)
	}
	if len(missing) > 0 {
		fmt.Fprintf(out, "Not set: %s. Set them in %s or as environment variables (SLACK_CHANNEL can also be slack.channel in the config file)\n",
			strings.Join(missing, ", "), envPath)
	}
	fmt.Fprintf(out, "\nEdit %s to adjust filters, then check with:\n  github-slack-notifications --dry-run --lookback 24h\n", configPath)
	return nil
}

func readLine(r *bufio.Reader, in *os.File, hidden bool, out io.Writer) (string, error) {
	if hidden && term.IsTerminal(int(in.Fd())) {
		b, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(out)
		return strings.TrimSpace(string(b)), err
	}
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// WriteConfig writes template to path, creating its directory, unless path
// already exists. It reports whether the file was written.
func WriteConfig(path string, template []byte) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := f.Write(template); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}

// ParseDotEnv returns the KEY=VALUE pairs in content.
func ParseDotEnv(content []byte) map[string]string {
	m := map[string]string{}
	for line := range strings.Lines(string(content)) {
		if k, v, ok := config.ParseDotEnvLine(line); ok {
			m[k] = v
		}
	}
	return m
}

// UpdateDotEnv replaces the lines of content that set a key in values and
// appends the remaining keys, keeping every other line.
func UpdateDotEnv(content []byte, values map[string]string) []byte {
	var b strings.Builder
	done := map[string]bool{}
	for line := range strings.Lines(string(content)) {
		k, _, ok := config.ParseDotEnvLine(line)
		if v, set := values[k]; ok && set {
			if !done[k] {
				fmt.Fprintf(&b, "%s=%s\n", k, v)
				done[k] = true
			}
			continue
		}
		b.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			b.WriteString("\n")
		}
	}
	for _, s := range secrets {
		if v, set := values[s.key]; set && !done[s.key] {
			fmt.Fprintf(&b, "%s=%s\n", s.key, v)
		}
	}
	return []byte(b.String())
}
