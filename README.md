# herdr-auto-resume

> **Fork notice:** this is a fork of [henryaj/autoclaude](https://github.com/henryaj/autoclaude)
> (MIT, Copyright (c) 2025 Henry Stanley — see LICENSE, preserved unmodified).
> This fork adds Herdr support behind a runtime abstraction. The original tmux TUI remains
> available.

![CI](https://github.com/Wave-Consulting-Netherlands/herdr-auto-resume/actions/workflows/ci.yml/badge.svg)

herdr-auto-resume monitors explicitly selected Herdr or tmux panes for Claude Code and Codex
usage limits, then schedules and verifies one provider-specific continuation when the limit
resets.

## Install

Release tarballs are published on the GitHub Releases page:
https://github.com/Wave-Consulting-Netherlands/herdr-auto-resume/releases

Each archive contains the herdr-auto-resume binary. Replace the installed binary in place when
upgrading.

Install tagged source with Go:

    go install github.com/Wave-Consulting-Netherlands/herdr-auto-resume@v0.3.0

Or build from a checkout:

    git clone https://github.com/Wave-Consulting-Netherlands/herdr-auto-resume.git
    cd herdr-auto-resume
    go build -o ~/.local/bin/herdr-auto-resume .

Requirements: Go 1.23 or newer when building, Herdr 0.7.5 or compatible protocol 17 for the
Herdr runtime, and tmux only when using the tmux runtime or TUI.

## Quickstart

Headless Herdr mode requires explicit pane selection:

    herdr-auto-resume run --pane w1:p1
    herdr-auto-resume doctor
    herdr-auto-resume status

Use a second pane or state file for an independent watcher. The default Herdr state is under
the XDG state directory. A watcher holds <absolute-state-file>.run; a second watcher on the
same state file fails fast. --state-file off disables persistence and the run lock.

## Running it

### Pane mode

The bare command starts the original tmux TUI. Use tab to toggle a pane, a/n to enable or
disable all panes, r to refresh, h/? for help, and q to quit.

For a headless pane watcher:

    herdr-auto-resume run --pane w1:p1 --pane w2:p1 --interval 5s
    herdr-auto-resume run --wait-for-panes --pane w1:p1

For the Herdr runtime, socket transport is now the built-in default when no --session is set
and no transport was explicitly requested. Use --transport cli as the opt-out. A
runtime.transport key in YAML is also explicit. When transport is not explicit, the helper
falls back to CLI with a warning naming the cause if runtime is tmux or --session is set.
Explicitly requesting an impossible combination still errors: socket with tmux, or socket with
--session. --session is not supported with socket transport.

### systemd user service

    mkdir -p ~/.config/systemd/user
    cp packaging/systemd/herdr-auto-resume.service ~/.config/systemd/user/
    systemctl --user daemon-reload
    systemctl --user enable --now herdr-auto-resume.service

On a headless host, enable lingering first. This is mandatory so the user service survives
logout:

    loginctl enable-linger "$USER"

The unit uses the socket default plus --wait-for-panes, a user-service PATH,
restart-on-failure, and NoNewPrivileges=yes. Edit YAML and restart the service; do not edit
the unit for normal configuration changes.

### launchd example

packaging/launchd/nl.wave-consulting.herdr-auto-resume.plist is example-only and untested.
Replace your-user and the binary/config paths, then load it with the normal per-user
launchctl bootstrap workflow. Its stdout and stderr are under ~/Library/Logs.

## Operations

    herdr-auto-resume status
    herdr-auto-resume inspect <job-id-prefix>
    herdr-auto-resume cancel <job-id-prefix>
    herdr-auto-resume revive <session-id-prefix>
    herdr-auto-resume doctor
    herdr-auto-resume doctor --transport socket --socket ~/.config/herdr/herdr.sock
    herdr-auto-resume detect --provider claude --file path/to/pane-capture.txt
    herdr-auto-resume detect --provider codex --file path/to/codex-capture.txt

Job commands read configured state.file when --state-file is omitted. doctor reports version,
config, watcher-lock, Herdr, adapter, schema, and self-pane diagnostics. Run-lock errors name
the holder PID; use another --state-file for a second watcher.

`revive` resolves a unique Claude session-file prefix, refuses if any pane already carries that
session, takes a non-blocking per-session lease, records crash-recovery intent, and starts
`claude --resume` in a new Herdr workspace. It requires the Herdr runtime and a persistent
state file. It sends no continuation; once the pane is monitored, the normal detection and
verification path handles it.

## Configuration

The default file is ~/.config/herdr-auto-resume/config.yaml, or
$XDG_CONFIG_HOME/herdr-auto-resume/config.yaml. Use --config path for another file. The file
must contain version: 1; unknown keys are rejected.

Precedence is built-in defaults < config file < explicitly set flags. A flag set explicitly to
its built-in value still wins. If the default config is absent, behavior is byte-identical to
flag-only behavior. See packaging/config.example.yaml for the full commented schema.

    version: 1
    runtime:
      type: herdr
      herdr_bin: herdr
      socket: ~/.config/herdr/herdr.sock
      workspace: your-workspace
    monitoring:
      panes: [w1:p1]
      interval: 3s
      lines: 200
      wait_for_panes: false
      admit_session_matches: false
    resume:
      margin: 60s
      max_wait: 192h
      verify_timeout: 90s
      answer_limit_menu: false
    providers:
      enabled: [claude, codex]
      claude_prompt: continue
      codex_prompt: Continue the previous task from where you stopped.
      session_file_channel: false
    state:
      file: auto

Run flags: --config, --runtime, --transport, repeatable --pane, --interval, --lines,
--wait-for-panes, --dry-run, --test-pattern, --herdr-bin, --socket, --session, --workspace,
--state-file, --margin, --max-wait, --verify-timeout, --providers, --session-file-channel,
--admit-session-matches, --answer-limit-menu, --claude-prompt, and --codex-prompt.

Doctor flags: --config, --transport, --herdr-bin, --socket, --session, --workspace, and
--state-file. Job flags: --config and --state-file. Detect flags: --file and
--provider claude|codex.

### v0.3.0 options

- `--wait-for-panes` / `monitoring.wait_for_panes`: default `false`. At startup, retries
  retryable reachability failures (connection refused, absent socket, timeout, or EOF) and zero
  matching panes with a rate-limited, signal-aware loop. Permanent protocol, malformed-response,
  permission/authentication, and configuration errors still fail fast.
- `--session-file-channel` / `providers.session_file_channel`: default `false`. Reads Claude
  session files for rate-limit observations and correlates them by `agent_session`; it requires
  a persistent state file and is rejected with the tmux runtime.
- `--admit-session-matches` / `monitoring.admit_session_matches`: default `false`. Per episode,
  admits an otherwise unmonitored Herdr pane only when exactly one live pane's `agent_session`
  matches the observation and the existing provider, cwd, self-pane, and validation gates pass.
  It requires `--session-file-channel`, a persistent state file, and the Herdr runtime.
- `--answer-limit-menu` / `resume.answer_limit_menu`: default `false`. For a manual Claude
  limit menu, it is single-shot per episode and only answers when the literal question, the text
  `Stop and wait for limit to reset`, and the cursor marker on that line are all present. It
  never selects by option index, requires a persistent state file and the Herdr runtime, and is
  subject to the read-then-send TOCTOU caveat because Herdr has no revision-conditional send.

These session-identity features remain opt-in and are not enabled by the shipped service
examples. The YAML keys must use the exact nesting shown above; unknown keys are rejected.

## Upgrade

Download the new release, replace ~/.local/bin/herdr-auto-resume, then restart the pane watcher
or user service. State files remain schema-compatible. In addition to the store's
<state>.run single-watcher lease and <state>.lock transaction lock, the session-file channel
uses <state>.scan.json for cursors/pending state and <state>.scan.lock for its exclusive
sidecar lock. `revive` also uses <state>.revive.<session-id>.lock for its per-session lease.

Semantic versioning applies: config-schema changes are minor releases and fixes are patch
releases. The v0.3.0 release flips the Herdr default to socket after the production soak and
aged-connection drill. Phase 7 validation targets Herdr 0.7.5/protocol
17; release notes record the Claude Code and Codex versions used for each live acceptance run
(Codex 0.144/0.146 were covered in Phase 5 validation).

## Troubleshooting

Start with herdr-auto-resume doctor. If a run says the state file is already in use, the named
PID owns the watcher lock. Confirm it, stop it if necessary, or choose another state file. Do
not delete .run while a watcher may still be running. A missing default config is normal and
is reported as INFO config: none; an explicitly requested missing or invalid config is an error.

For socket problems, run doctor --transport socket --socket ... and verify Herdr protocol 17.
For a service that restarts, inspect journalctl --user -u herdr-auto-resume.service and check
the binary path, PATH, pane IDs, and loginctl show-user "$USER" -p Linger.

## How it works

When enabled, the coordinator uses two detection channels: Claude session files are authoritative
where a durable record exists, while screen scraping remains the fallback for visible pane evidence.
Both channels resolve the same provider/session/reset episode identity, so delayed or duplicate
evidence does not create duplicate jobs. A limited pane that yields no job logs one diagnostic
line per evidence hash naming the reason. The scheduler validates provider/process/session
identity, sends the provider-specific continuation, and verifies cleared evidence or changed
output. State remains JSON schema 1; the store and scan sidecars serialize their own short
transactions.

## Development

    export PATH=$HOME/.local/go/bin:$PATH
    export GOCACHE=/tmp/herdr-go-cache
    export GOMODCACHE=/tmp/herdr-go-modcache
    go build ./...
    go vet ./...
    go test ./... -race -count=1

## License

MIT License — see LICENSE. Upstream attribution is preserved unmodified.

## Credits

Forked from Henry Stanley's autoclaude and built with Herdr, Claude Code, and Codex.
