# Packaging and deferred evaluations

## Release flow

The release identity is herdr-auto-resume. GoReleaser injects the semantic version, short
commit, and build date into the binary; CI runs both GoReleaser configuration checking and a
publish-skipped snapshot. The release script guards the working tree and existing tag, creates
and pushes the tag, watches the release workflow, and downloads checksums. The v0.3.0 release
is the socket-default flip after the production soak and aged-connection drill. No Homebrew tap
is maintained by this repository.

Normal upgrades replace the binary and restart the pane watcher or user service. The JSON state
schema is unchanged. Herdr uses the socket transport by default when no session or explicit
transport is set; `--transport cli` remains the explicit opt-out. The default helper falls back
to CLI with a warning for tmux or `--session`, while explicit impossible socket combinations
still fail. A YAML `runtime.transport` key is explicit too.

The store's `<state>.run` prevents two watchers from owning one state file and `<state>.lock`
serializes store transactions. The optional Claude session-file channel uses
`<state>.scan.json` and `<state>.scan.lock`; `revive` additionally uses a per-session
`<state>.revive.<session-id>.lock`. Session-file detection, targeted admission, and menu
answering remain opt-in configuration features and are not enabled by the shipped units.

## Herdr plugin inventory and decision

A live Herdr 0.7.5 plugin-help inventory showed install, link, enable, list, config-dir,
action invoke, log, and plugin-owned panes capabilities. This could eventually wrap the
existing herdr-auto-resume binary, but there is no stable startup/event-hook or schema method
contract for this project, and the 0.7.x target is moving.

Decision D-P7-3: defer plugin packaging. A future wrapper should invoke the existing binary,
pass the configured state/config paths, and treat the socket API as the runtime boundary. It
must not duplicate detection, persistence, or provider safety logic.

## Herdr-native TUI decision

Decision D-P7-4: defer a Herdr-native TUI. The daemon and CLI provide status, inspect, doctor,
and operations views, while the original tmux Bubble Tea TUI preserves upstream behavior. A
future TUI can consume those command/API boundaries after the headless lifecycle is stable.

## Service examples

The systemd user unit is the first-class headless service mode. It uses the socket default,
`--wait-for-panes`, a user-unit PATH, restart-on-failure, and NoNewPrivileges. The wait flag
retries transient reachability failures and zero matching panes at startup, but permanent
protocol, permission, malformed-response, and configuration failures still stop the unit.
Headless hosts must run `loginctl enable-linger USER`. The launchd plist is an example-only,
untested translation with absolute placeholder paths, the same socket default, and logs under
`~/Library/Logs`.
