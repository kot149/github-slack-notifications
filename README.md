# github-slack-notifications

A long-running program that filters your GitHub notifications and forwards them to a Slack channel.

## How it works

- Polls the GitHub Notifications API every `X-Poll-Interval` (usually 60 seconds)
  - Requests carry `If-Modified-Since`, so a poll with no new notifications ends with `304` and doesn't consume rate limit
- Filters notifications updated since the last fetch by reason, repository and event, then posts them to Slack
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

| Kind | Shown as | Event (for `mute.events`) |
|---|---|---|
| Merge / close | `:twisted_rightwards_arrows: Merged PR` / `:no_entry_sign: Closed PR` | `merged` / `closed` |
| Review | `:white_check_mark: Approved by X` / `:warning: Changes requested by X` / `:speech_balloon: New review comment by X` / `:speech_balloon: New reply to review comment by X` | `approved` / `changes_requested` / `review_comment` / `review_reply` |
| Comment | `:speech_balloon: New comment by X` | `comment` |
| By reason | `:eyes: Review requested` / `:point_right: Assigned to PR` / `:mega: Mentioned in PR` | `review_requested` / `assigned` / `mentioned` |
| Edit | `:pencil2: Edited PR description by X` / `:pencil2: Edited comment by X` / `:pencil2: Edited review by X` / `:pencil2: Edited review comment by X` / `:pencil2: Renamed PR by X` | `edited` (all but rename) / `renamed` |
| PR state | `:recycle: Reopened PR by X` / `:arrow_forward: Ready for review by X` / `:construction: Converted to draft by X` / `:arrow_up: Force-pushed by X` / `:arrow_right_hook: Changed base branch by X` / `:wastebasket: Review dismissed by X` | `reopened` / `ready_for_review` / `converted_to_draft` / `force_pushed` / `base_changed` / `review_dismissed` |
| Other | `:sparkles: Opened PR` / `:arrows_counterclockwise: Updated PR` / `:bell:` for other notification types | `opened` / `updated` / `other` |
| CI | Only for PRs you opened, one item per PR. `:red_circle: CI failed (2/15)` with the list of failed checks, `:hourglass_flowing_sand: CI running (8/15 done)`, `:large_green_circle: CI all green (15)` | `ci_failed` / `ci_running` / `ci_all_green` |
| Release | `:rocket: Release` | `release` |

- The cause of an update is determined from activity since you last read the thread (`last_read_at`)
- Edits and PR state changes are checked only when no new comment, review, merge, close or reason explains the update. `Updated PR` remains for updates nothing explains
- Your own comments, reviews, edits and state changes are not forwarded
- `mute.events` lists events not to forward, and `mute.reasons` and `mute.repositories` list [notification reasons](https://docs.github.com/en/rest/activity/notifications#about-notification-reasons) and `owner/repo` (case-insensitive) not to forward. A muted event is dropped like your own activity, so other activity at the same moment, such as a comment with a merge, is still forwarded. With `mute.read: true`, notifications already read on GitHub are not forwarded
- With `ci_only_on_change: true`, CI is forwarded only when a PR's state (failed / running / all green) differs from the last one posted for it. A muted CI state isn't counted as posted, so with `ci_running` muted, a rerun or new push that ends with the same result as the last post is not forwarded either
- `ci_ignore_checks` maps `owner/repo` to workflow names (e.g. `Spell check`) or check run names (e.g. `spell-check`) that are left out of the CI result and counts. Names are case-insensitive
- Updates that arrive after an already forwarded merge or close (e.g. branch deletion) are not forwarded

## Setup

1. Install

   ```sh
   go install github.com/kot149/github-slack-notifications@latest
   ```

   Or clone this repository and run `go build -o github-slack-notifications .`

2. Run `init`, which places `config.yml` (from [config.example.yml](config.example.yml)) and asks for the values below to save in `.env.local` next to it (tokens are typed without echo)

   ```sh
   github-slack-notifications init
   ```

   - `GITHUB_TOKEN`: a classic PAT with the `notifications` and `repo` scopes, authorized for your org's SSO. Without `repo`, notifications from private repositories are posted without details
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
github-slack-notifications config get mute.read            # one effective value
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
