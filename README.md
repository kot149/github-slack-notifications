# github-slack-notifications

A long-running program that filters your GitHub notifications and forwards them to a Slack channel.

## How it works

- Polls the GitHub Notifications API every `X-Poll-Interval` (usually 60 seconds)
  - Requests carry `If-Modified-Since`, so a poll with no new notifications ends with `304` and doesn't consume rate limit
- Filters notifications updated since the last fetch by reason and repository, then posts them to Slack
- Marks forwarded notifications as read (`mark_as_read`)
- Keeps its state (last fetch time, handled notifications) in `state_file`
- If fetching from GitHub keeps failing for 15 minutes (e.g. the token expired), posts a warning to the same channel, and a follow-up once it recovers

### Messages

Each notification takes two lines. With `rollup: true`, all notifications found in one poll are combined into a single message.

```
*:white_check_mark: Approved by alice*      ← links to the review
owner/repo #123 PR title                    ← links to the PR
```

The first line links to the comment or review for comment and review events, and to the PR or issue otherwise.

| Kind | Shown as |
|---|---|
| Merge / close | `:twisted_rightwards_arrows: Merged PR` / `:no_entry_sign: Closed PR` |
| Review | `:white_check_mark: Approved by X` / `:warning: Changes requested by X` / `:speech_balloon: New review comment by X` |
| Comment | `:speech_balloon: New comment by X` |
| By reason | `:eyes: Review requested` / `:point_right: Assigned to PR` / `:mega: Mentioned in PR` |
| Other | `:sparkles: Opened PR` / `:arrows_counterclockwise: Updated PR` |
| CI | One item per PR. `:red_circle: CI failed (2/15)` with the list of failed checks, `:hourglass_flowing_sand: CI running (8/15 done)`, `:large_green_circle: CI all green (15)` |
| Release | `:rocket: Release` |

- The cause of an update is determined from activity since you last read the thread (`last_read_at`)
- Updates that arrive after an already forwarded merge or close (e.g. branch deletion) are not forwarded

## Setup

1. Install

   ```sh
   go install github.com/kot149/github-slack-notifications@latest
   ```

   Or clone this repository and run `go build -o github-slack-notifications .`

2. Run `init`, which places [config.yml](config.yml) and asks for the values below to save in `.env.local` next to it (tokens are typed without echo)

   ```sh
   github-slack-notifications init
   ```

   - `GITHUB_TOKEN`: a classic PAT with the `notifications` scope, authorized for your org's SSO
   - `SLACK_TOKEN`: a token for a bot invited to the target channel. Changing `username` and the icon requires the `chat:write.customize` scope
   - `SLACK_CHANNEL`: the target channel ID or user ID (can also be set as `slack.channel` in `config.yml`)

   An existing config file is kept, and pressing Enter keeps a value already in `.env.local`. Environment variables can be used instead of `.env.local`.

   Values passed as flags are saved without prompting, e.g. `init --slack-channel C0123456789`. `--github-token` and `--slack-token` work the same way, but leave the tokens in shell history.

3. Edit `config.yml` to configure filters and other options

   - The config file is `./config.yml` if it exists, otherwise `$XDG_CONFIG_HOME/github-slack-notifications/config.yml` (`~/.config` when unset). `--config` overrides it, for `init` too
   - `.env.local` and a relative `state_file` are read from the config file's directory

## Usage

```sh
github-slack-notifications                          # keep running and poll
github-slack-notifications --once                    # check once and exit
github-slack-notifications --dry-run --lookback 24h  # only print messages for the last 24 hours (no posting, marking as read, or saving state)
github-slack-notifications --config path/to/config.yml
```

### Inspecting and changing the config

```sh
github-slack-notifications config path                     # where the config file, .env.local and state file are
github-slack-notifications config get                      # effective config with defaults and environment applied, tokens masked
github-slack-notifications config get filter.only_unread   # one effective value
github-slack-notifications config set rollup false         # value is YAML, e.g. '[owner/a, owner/b]'
github-slack-notifications config edit                     # open in $VISUAL or $EDITOR, then validate
```

`set` keeps comments (and the rest of the file as is when the value fits on the key's line), and rejects unknown keys and values of the wrong type. Put `--config path` right after `config` to target another file.

### Running with launchd

`~/Library/LaunchAgents/local.github-slack-notifications.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>local.github-slack-notifications</string>
    <key>ProgramArguments</key>
    <array>
        <string>/Users/you/go/bin/github-slack-notifications</string>
    </array>
    <key>KeepAlive</key>
    <true/>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardErrorPath</key>
    <string>/Users/you/.config/github-slack-notifications/forwarder.log</string>
</dict>
</plist>
```

```sh
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/local.github-slack-notifications.plist
```
