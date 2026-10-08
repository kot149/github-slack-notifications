package main

import (
	"bufio"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/kot149/github-slack-notifications/internal/config"
)

const configUsage = `Usage: %s config [--config path] <command>

Commands:
  path               print the config file, .env.local and state file paths
  get [key]          print the effective value of a key, e.g. filter.only_unread,
                     or without a key the whole config with tokens masked
  set <key> <value>  set a key in the config file; value is YAML, e.g. true or [o/a, o/b]
  edit               open the config file in $VISUAL or $EDITOR and validate it

Flags:
`

func runConfig(args []string) {
	flags := flag.NewFlagSet("config", flag.ExitOnError)
	configPath := flags.String("config", config.DefaultPath(), "path to the config file")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), configUsage, os.Args[0])
		flags.PrintDefaults()
	}
	flags.Parse(args)
	args = flags.Args()

	// Allowed argument counts after the command.
	want := map[string][]int{"path": {0}, "get": {0, 1}, "set": {2}, "edit": {0}}
	if len(args) == 0 || !slices.Contains(want[args[0]], len(args)-1) {
		flags.Usage()
		os.Exit(2)
	}

	path := *configPath
	var err error
	switch args[0] {
	case "path":
		err = printPaths(path)
	case "get":
		if len(args) == 1 {
			err = printConfig(path)
			break
		}
		var cfg *config.Config
		if cfg, err = config.Resolve(path); err == nil {
			var v string
			if v, err = config.Get(cfg, args[1]); err == nil {
				fmt.Println(v)
			}
		}
	case "set":
		err = config.Set(path, args[1], args[2])
	case "edit":
		err = configEdit(path)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func printPaths(path string) error {
	cfg, err := config.Parse(path)
	if errors.Is(err, fs.ErrNotExist) {
		cfg, err = config.ParseBytes(path, nil)
	}
	if err != nil {
		return err
	}
	for _, f := range [][2]string{{"config", path}, {".env.local", config.EnvPath(path)}, {"state_file", cfg.StateFile}} {
		abs, err := filepath.Abs(f[1])
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); errors.Is(err, fs.ErrNotExist) {
			abs += " (not found)"
		}
		fmt.Printf("%-11s %s\n", f[0]+":", abs)
	}
	return nil
}

func printConfig(path string) error {
	inEnv := map[string]bool{}
	for _, k := range []string{"GITHUB_TOKEN", "SLACK_TOKEN"} {
		_, inEnv[k] = os.LookupEnv(k)
	}
	cfg, err := config.Resolve(path)
	if err != nil {
		return err
	}
	fmt.Printf("# %s\n", path)
	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return err
	}
	enc.Close()
	fmt.Println()
	for _, t := range [][2]string{{"GITHUB_TOKEN", cfg.GitHubToken}, {"SLACK_TOKEN", cfg.SlackToken}} {
		desc := "not set"
		if t[1] != "" {
			desc = maskToken(t[1]) + " (from .env.local)"
			if inEnv[t[0]] {
				desc = maskToken(t[1]) + " (from environment)"
			}
		}
		fmt.Printf("%-13s %s\n", t[0]+":", desc)
	}
	return nil
}

// maskToken keeps only the type prefix, e.g. ghp_ or xoxb-.
func maskToken(s string) string {
	i := strings.IndexAny(s, "_-")
	if i < 0 || i > 5 {
		return "****"
	}
	return s[:i+1] + "****"
}

func configEdit(path string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s does not exist; run `%s init` first", path, filepath.Base(os.Args[0]))
		}
		return err
	}
	editor := strings.Fields(cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi"))
	in := bufio.NewReader(os.Stdin)
	for {
		cmd := exec.Command(editor[0], append(editor[1:], path)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
		_, err := config.Parse(path)
		if err == nil {
			return nil
		}
		fmt.Fprintf(os.Stderr, "%v\nEdit again? [Y/n] ", err)
		ans, rerr := in.ReadString('\n')
		if rerr != nil || strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "n") {
			fmt.Fprintln(os.Stderr)
			return err
		}
	}
}
