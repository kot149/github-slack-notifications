# AGENTS.md

## Deploy every change

The forwarder runs locally as the launchd job `com.kota.github-notifications-forwarder`, which executes the `github-slack-notifications` binary in the repo root. After every code change, always rebuild and restart it so the running job picks up the change:

```sh
rm -f github-slack-notifications && go build -o github-slack-notifications . && launchctl kickstart -k gui/$(id -u)/com.kota.github-notifications-forwarder
```

Remove the old binary before building. Overwriting it in place makes macOS reject the new one with `OS_REASON_CODESIGNING`, and the job fails to start.

Then confirm the job is running with `launchctl list | grep github-notifications` (a PID and exit status 0) and that `forwarder.log` shows no new errors.
