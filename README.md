# github-slack-notifications

A long-running program that filters your GitHub notifications and forwards them to a Slack channel.

## How it works

- Polls the GitHub Notifications API every `X-Poll-Interval` (usually 60 seconds)
  - Requests carry `If-Modified-Since`, so a poll with no new notifications ends with `304` and doesn't consume rate limit
- Filters notifications updated since the last fetch by reason and repository, then posts them to Slack
- Marks forwarded notifications as read (`mark_as_read`)
- Keeps its state (last fetch time, handled notifications) in `state_file`

### Messages

Each notification takes two lines. With `rollup: true`, all notifications found in one poll are combined into a single message.

```
*:white_check_mark: Approved by alice*      ← links to the PR
owner/repo #123 PR title
```

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

1. Put the following in `.env.local` (environment variables work too)
   - `GITHUB_TOKEN`: a classic PAT with the `notifications` scope, authorized for your org's SSO
   - `SLACK_TOKEN`: a token for a bot invited to the target channel. Changing `username` and the icon requires the `chat:write.customize` scope
   - `SLACK_CHANNEL`: the target channel ID or user ID (can also be set as `slack.channel` in `config.yml`)

   ```
   GITHUB_TOKEN=ghp_...
   SLACK_TOKEN=xoxb-...
   SLACK_CHANNEL=C0123456789
   ```

2. Configure filters and other options in [config.yml](config.yml)
3. Build

   ```sh
   go build -o github-slack-notifications .
   ```

## Usage

```sh
./github-slack-notifications                         # keep running and poll
./github-slack-notifications -once                   # check once and exit
./github-slack-notifications -dry-run -lookback 24h  # only print messages for the last 24 hours (no posting, marking as read, or saving state)
```

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
        <string>/path/to/github-slack-notifications/github-slack-notifications</string>
    </array>
    <key>WorkingDirectory</key>
    <string>/path/to/github-slack-notifications</string>
    <key>KeepAlive</key>
    <true/>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardErrorPath</key>
    <string>/path/to/github-slack-notifications/forwarder.log</string>
</dict>
</plist>
```

```sh
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/local.github-slack-notifications.plist
```
