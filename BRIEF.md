# Herdr Auto Resume

## Project brief

**Working title:** herdr-auto-resume
**Document status:** Initial implementation brief
**Target environment:** Herdr on Linux/macOS, with primary focus on Ubuntu VPS usage
**Supported agents for MVP:** Claude Code and OpenAI Codex CLI
**Recommended implementation language:** Go
**Recommended starting repository:** henryaj/autoclaude
**Reference implementations:** alexei-led/ccgram, cheapestinference/claude-auto-retry, and herdrdev/herdr

────────

## 1. Summary

Create a small Herdr-native service that monitors Claude Code and Codex sessions running in Herdr panes. When an agent stops because its subscription usage window has been exhausted, the service should:

1. Detect that the agent has reached a genuine usage limit.
2. Determine when the limit resets.
3. Persist the pending resume operation.
4. Wait without consuming significant resources.
5. Revalidate the pane, process, provider, and agent session after the reset.
6. Submit a safe continuation instruction exactly once.
7. Continue monitoring the session for later limits or completion.

The tool must work while the user is detached from Herdr or disconnected from SSH. It must not require tmux inside Herdr.

This project does not bypass Claude or Codex limits. It only automates the manual action the user would otherwise perform after the provider makes capacity available again.

────────

## 2. Problem statement

Long-running implementation, refactoring, migration, testing, and documentation tasks may take longer than the usage window available to Claude Code or Codex. When a limit is reached, the coding agent stops and displays a message indicating that the user must wait until a reset time.

Herdr already solves terminal persistence:

- The agent process remains in its pane while the user is detached.
- Work can continue through SSH disconnects.
- The user can reattach and inspect the same terminal state.
- Herdr exposes pane and agent operations through its CLI and local socket API.

However, terminal persistence alone does not resume an agent after its limit resets. The user must still return and manually submit `continue` or an equivalent prompt. For overnight, unattended, or multi-agent work, that can leave a task idle for several hours even though the usage window has already reset.

Existing auto-resume tools mostly assume tmux. Nesting tmux inside Herdr would duplicate responsibilities, complicate navigation, and make Herdr less useful as the primary agent runtime. The required solution should control Herdr directly.

────────

## 3. Goals

### Primary goals

- Support Claude Code and Codex CLI sessions running in Herdr panes.
- Detect provider usage-limit states with a low false-positive rate.
- Parse explicit reset times when available.
- Handle relative reset messages through a configurable fallback.
- Resume the same session after the reset without user intervention.
- Persist waiting state across watcher restarts.
- Support multiple simultaneous Herdr panes and workspaces.
- Require explicit opt-in per pane, agent, workspace, or global policy.
- Verify that the original agent still occupies the pane before injecting input.
- Provide useful status, logs, diagnostics, and notifications.
- Be usable on a headless Ubuntu VPS.

### Secondary goals

- Retain an abstract terminal/multiplexer interface so tmux support can remain available for regression testing or broader adoption.
- Support weekly and other non-five-hour usage limits when a reset can be determined.
- Support transient API overload backoff separately from subscription usage limits.
- Expose a provider interface so Gemini CLI or future agents can be added later.
- Optionally package the tool as a Herdr plugin after the standalone implementation is stable.

────────

## 4. Non-goals

The MVP will not:

- Circumvent, evade, or increase provider quotas.
- Purchase extra usage or select an upgrade option automatically.
- Restart an agent from scratch when the original session has disappeared.
- Guess an unknown continuation task from terminal scrollback alone.
- Automatically approve tool-use, destructive-operation, security, or permission prompts.
- Inject arbitrary shell commands into panes.
- Coordinate multiple agents working on the same branch.
- Guarantee semantic project continuity after context compaction or a lost agent session.
- Replace good project checkpointing, Git commits, task files, or progress documentation.
- Depend on a hosted service or expose the Herdr socket over the network.

────────

## 5. User scenario

A user starts Claude Code or Codex inside a Herdr pane on an Ubuntu VPS and gives it a substantial task. The user detaches from Herdr and disconnects SSH.

Several hours later, the agent reaches a subscription usage limit. Herdr keeps the pane alive, while herdr-auto-resume detects the limit and records the provider, agent identity, pane identity, session reference, reset time, and current project directory.

After the limit resets, the watcher confirms that:

- Herdr is still running.
- The tracked agent/session still exists.
- The pane still contains Claude Code or Codex.
- The process has not been replaced by a shell, editor, or another application.
- The terminal is still showing the expected blocked/idle state.
- No resume has already been submitted for this event.

It then submits a provider-specific continuation prompt and records the result. The user later reconnects and finds the agent working or the task completed.

────────

## 6. Recommended fork strategy

### 6.1 Fork henryaj/autoclaude

autoclaude is the preferred starting point because it is small, MIT licensed, written in Go, and already contains the core behavior:

- Identify Claude Code panes.
- Detect a rate-limit message.
- Parse a reset time.
- Track waiting state.
- Send Escape, text, and Enter when the reset passes.
- Provide a terminal UI and a test-pattern mode.

Its current structure already separates detection, tmux, and TUI packages:

```text
internal/
├── detection/
├── tmux/
└── tui/
```

It is not yet transport-neutral: its application entry point validates tmux directly, and its user experience assumes tmux layout semantics. The fork should therefore retain the useful state machine and tests while refactoring the terminal integration behind an interface.

### 6.2 Use ccgram as the architecture reference

ccgram already supports both tmux and Herdr and has an explicit multiplexer boundary. Its concrete Herdr and tmux implementations are isolated behind a neutral multiplexer contract. It also supports provider abstractions for Claude Code and Codex.

Do not fork all of ccgram for this project. It includes Telegram messaging, web UI, routing, transcript handling, multi-topic state, and other features that are unnecessary for an auto-resume daemon. Instead, study and reimplement the relevant ideas:

- Herdr socket connection and reconnection.
- Pane discovery and output reading.
- Multiplexer abstraction.
- Foreground process/provider detection.
- Agent provider registry.
- Event subscription handling.
- Session recovery patterns.

Because ccgram is Python and the proposed project is Go, reuse should primarily be architectural. Any directly translated or copied implementation must retain appropriate MIT attribution.

### 6.3 Borrow hardened detection ideas from claude-auto-retry

claude-auto-retry has broader Claude limit handling than the small autoclaude codebase. Its design includes:

- Timezone-aware reset parsing.
- Daylight-saving-safe calculations.
- Configurable safety margin after reset.
- Weekly, session, usage, and relative-limit formats.
- Tail-focused detection to avoid matching old scrollback.
- Protection against quoted rate-limit messages.
- Foreground-process validation before input injection.
- Separate handling for exhausted API overload retries.
- Bounded retry behavior.
- Persistent or self-healing monitor coverage.

The new project should borrow the behavior and test cases that make sense, but should not inherit the tmux-specific launcher and shell integration.

### 6.4 Keep the fork history initially

Start as a real fork of autoclaude rather than copying its files into an unrelated repository. This preserves history and attribution. After the architecture has diverged substantially, the project can retain the fork relationship or become a standalone repository while preserving the original license and notices.

────────

## 7. Proposed architecture

```text
                         ┌───────────────────────────────┐
                         │ CLI / optional TUI            │
                         │ enable, disable, status, logs │
                         └──────────────┬────────────────┘
                                        │
                         ┌──────────────▼────────────────┐
                         │ Resume coordinator            │
                         │ state machine + scheduling    │
                         └───────┬───────────┬───────────┘
                                 │           │
                 ┌───────────────▼───┐   ┌──▼──────────────────┐
                 │ Provider registry │   │ Persistent state     │
                 │ Claude / Codex    │   │ pending resume jobs  │
                 └───────────┬───────┘   └─────────────────────┘
                             │
                 ┌───────────▼────────────┐
                 │ Runtime abstraction    │
                 │ panes, agents, input,  │
                 │ process info, events   │
                 └───────────┬────────────┘
                             │
                 ┌───────────▼────────────┐
                 │ Herdr adapter          │
                 │ CLI first / socket API │
                 └───────────┬────────────┘
                             │
                 ┌───────────▼────────────┐
                 │ Running Herdr server   │
                 └────────────────────────┘
```

### 7.1 Core components

**Resume coordinator**

Owns the lifecycle of monitored sessions. It should not know how Herdr transports requests or how individual providers render their UI.

Responsibilities:

- Discover eligible agents.
- Apply opt-in policy.
- Ask providers to classify current output/state.
- Create and persist resume jobs.
- Wake at the calculated reset time.
- Run safety validation.
- Execute the provider-specific resume action.
- Record success, cancellation, failure, and retry outcomes.

**Runtime abstraction**

Defines the minimum capabilities needed from Herdr or another multiplexer:

```go
type Runtime interface {
    ListPanes(ctx context.Context) ([]Pane, error)
    ListAgents(ctx context.Context) ([]Agent, error)
    ReadPane(ctx context.Context, paneID string, opts ReadOptions) (PaneOutput, error)
    ProcessInfo(ctx context.Context, paneID string) (ProcessInfo, error)
    SendText(ctx context.Context, paneID string, text string) error
    SendKeys(ctx context.Context, paneID string, keys ...string) error
    Notify(ctx context.Context, notification Notification) error
    Subscribe(ctx context.Context, filter EventFilter) (EventStream, error)
}
```

The exact method set may change after implementation experiments. Provider and coordinator packages must not import the concrete Herdr adapter.

**Provider interface**

Encapsulates all Claude- or Codex-specific behavior:

```go
type Provider interface {
    Name() string
    DetectProcess(info ProcessInfo) bool
    DetectLimit(ctx DetectionContext) LimitDetection
    ValidateResumeState(ctx ValidationContext) ValidationResult
    BuildResumeAction(ctx ResumeContext) ResumeAction
}
```

A ResumeAction should contain only structured, allow-listed terminal operations:

```go
type ResumeAction struct {
    KeysBefore []string
    Text       string
    SubmitKey  string
}
```

Do not let provider configuration execute shell commands.

**Persistent store**

For MVP, use an atomic JSON file under the user configuration/state directory. SQLite can be introduced later if event history, concurrent writers, or querying requirements justify it.

Suggested location:

```text
~/.local/state/herdr-auto-resume/state.json
```

Persist at least:

- Resume-event UUID.
- Provider.
- Herdr workspace, tab, and pane IDs.
- Herdr agent/session identity when available.
- Foreground executable and command line fingerprint.
- Working directory.
- Detection timestamp.
- Raw and normalized reset specification.
- Calculated UTC resume time.
- Configured safety margin.
- Current state.
- Attempt count.
- Last validation result.
- Last error.
- Timestamp and hash of the terminal evidence that triggered detection.

Use write-to-temporary-file plus atomic rename to prevent corruption.

────────

## 8. Herdr integration

Herdr exposes the control surface needed by this project through CLI wrappers and a local socket API. Relevant methods include:

- `session.snapshot`
- `pane.list`
- `pane.get`
- `pane.read`
- `pane.process_info`
- `pane.send_text`
- `pane.send_keys`
- `pane.wait_for_output`
- `agent.list`
- `agent.get`
- `agent.read`
- `agent.prompt`
- `agent.wait`
- `events.subscribe`
- `events.wait`
- `notification.show`

### 8.1 MVP integration

Use Herdr CLI wrappers for the first working implementation. This keeps protocol handling simple and makes commands easy to reproduce while debugging.

Example conceptual operations:

```bash
herdr api snapshot
herdr pane read w1:p1 --source recent --lines 80
herdr pane process-info w1:p1
herdr pane send-text w1:p1 "continue"
herdr pane send-keys w1:p1 enter
```

The exact CLI syntax must be checked against the installed Herdr version by using:

```bash
herdr api schema
herdr api schema --json
```

Do not hard-code assumptions that conflict with the schema shipped by the installed Herdr binary.

### 8.2 Production integration

Move the long-running watcher to the raw local socket API after the behavior is proven. A persistent socket client is preferable for:

- Long-lived event subscriptions.
- Lower process-launch overhead.
- Faster detection.
- Atomic request/response handling.
- Explicit reconnect and resynchronization behavior.

On connect or reconnect:

1. Request `session.snapshot`.
2. Rebuild the local pane/agent cache.
3. Subscribe to relevant resource, output, process, and agent events.
4. Reconcile persisted resume jobs against the live session.
5. Fall back to periodic polling as a safety net.

### 8.3 Pane identity and movement

A pane can be moved, closed, restored, or replaced. Public pane IDs may not be sufficient as the only durable identity.

A pending resume job should therefore retain several matching signals:

- Herdr agent/session reference, when available.
- Provider name.
- Foreground executable and arguments.
- Project working directory.
- Original workspace/tab/pane IDs.
- A fingerprint of the rate-limit evidence.

If the session can be resolved after a pane move, update the stored pane ID. If identity is ambiguous, stop and require manual intervention rather than sending input to a guessed pane.

### 8.4 Herdr plugin option

A later release may be packaged as a Herdr plugin. Potential benefits:

- Startup hook after Herdr restore.
- Event hooks without a separately managed service.
- Plugin actions such as Enable Auto Resume, Disable Auto Resume, and Resume Now.
- Persistent plugin state in the Herdr plugin state directory.
- Native integration with Herdr notifications and agent views.

A standalone daemon is preferred for the MVP because it is easier to debug, release independently, and test against multiple Herdr versions.

────────

## 9. Detection strategy

Detection is the highest-risk part of the tool. A false negative leaves a task waiting; a false positive can inject text into the wrong terminal state. The design must prefer false negatives over unsafe false positives.

### 9.1 Evidence priority

Use the strongest available signal in this order:

1. Provider lifecycle hook explicitly identifying a usage/rate-limit stop.
2. Herdr-recognized provider and agent state plus matching live terminal output.
3. Provider process detection plus matching live terminal output.
4. Terminal output alone only when strict safeguards are satisfied.

Hooks should supplement, not completely replace, visible-output verification. Provider hook behavior and formats can change.

### 9.2 Live-tail detection

Only inspect the visible terminal and a limited recent tail. Do not search unlimited scrollback.

The detector should:

- Normalize ANSI sequences and terminal control characters.
- Preserve line boundaries.
- Understand enough terminal rendering to avoid matching hidden/stale text.
- Ignore matches that have newer normal agent output beneath them.
- Ignore code blocks, logs, documentation, or prompts that merely quote a limit message.
- Require provider identity before treating a match as actionable.
- Store the exact matched evidence for diagnostics.

A full VT100 emulator may not be necessary for the MVP, but ccgram demonstrates that terminal rendering becomes important when coding-agent TUIs overwrite lines or maintain widgets at the bottom of the pane.

### 9.3 Reset parsing

Represent reset information as a typed value rather than a string:

```go
type ResetSpec struct {
    Kind       ResetKind // absolute, local-clock, relative, date-time, unknown
    Raw        string
    Timezone   string
    ParsedTime time.Time
    Confidence Confidence
}
```

Handle at least:

- Clock time with AM/PM.
- Clock time with an IANA timezone.
- Date plus time for weekly limits.
- Relative duration such as a number of hours.
- Missing reset time through a configurable fallback.

Rules:

- Convert the final resume time to UTC for storage.
- Retain the original timezone and raw text.
- If only a clock time is supplied and that time has already passed locally, interpret it as the next occurrence.
- Account for daylight-saving transitions.
- Add a configurable margin, defaulting to 60 seconds.
- Reject implausible reset times rather than waiting for an obviously incorrect date.
- Allow a maximum wait horizon, for example eight days, to prevent malformed parsing from creating effectively permanent jobs.

### 9.4 Claude Code provider

Initial Claude support should recognize the established families of messages rather than one exact phrase:

- Five-hour or N-hour limit reached.
- Session limit reached.
- Usage limit reached.
- Weekly limit reached.
- Out of extra usage.
- Explicit reset time.
- Relative retry period.

Claude may display a rate-limit options menu. The tool must never select an upgrade or payment option. It may only choose a clearly identified wait/stop option when the layout can be understood with high confidence. If the menu is ambiguous, record the job as requiring manual intervention.

Typical continuation action:

```text
Escape → type continuation prompt → Enter
```

The exact key sequence should be based on the detected UI state and covered by fixtures.

### 9.5 Codex provider

Codex support must be implemented independently rather than assuming Claude patterns or key sequences.

Initial work should collect fixtures for:

- Five-hour usage exhaustion.
- Weekly usage exhaustion, if exposed.
- Explicit reset time.
- Relative retry time.
- Normal task completion.
- Approval or permission prompts.
- Login/authentication failures.
- Context or session errors.
- Transient API overloads.

The provider should use Codex lifecycle hooks or transcript information when available, while retaining terminal verification. Exact Codex resume semantics must be tested against the installed CLI version. The resume action may be `continue`, a longer continuation prompt, or a provider-native resume command.

Recommended default continuation prompt:

```text
Continue the previous task from where you stopped. First inspect the current repository state and existing progress before making further changes.
```

The user must be able to override this prompt per provider.

### 9.6 Transient overloads

Subscription usage limits and transient service errors are different states:

- Usage limit: wait until the provider-supplied reset time.
- Service overload: use bounded exponential backoff with jitter.
- Authentication or billing error: do not retry automatically.
- Permission or safety prompt: do not retry automatically.

Do not interfere while the agent is already performing its own internal retry sequence. Only act when the provider has stopped retrying and presents a stable terminal error.

────────

## 10. Resume state machine

```text
UNTRACKED
   │ opt-in/discovery
   ▼
MONITORING
   │ limit detected
   ▼
RATE_LIMITED
   │ reset parsed and persisted
   ▼
WAITING
   │ watcher restart ─────────────┐
   │ reset time reached           │ restore/reconcile
   ▼                              │
VALIDATING ◄──────────────────────┘
   │ safe                         │ unsafe/ambiguous
   ▼                              ▼
RESUMING                    MANUAL_REQUIRED
   │ input submitted
   ▼
VERIFYING_RESUME
   │ agent active/output changed
   ▼
MONITORING
```

Terminal states:

- DISABLED
- CANCELLED
- FAILED
- MANUAL_REQUIRED
- SESSION_GONE

### Validation before resume

All of the following should pass:

- The job has not been disabled or cancelled.
- The scheduled time plus safety margin has passed.
- The event has not already been resumed.
- The Herdr server is reachable.
- The expected agent/session can still be resolved.
- The provider matches.
- The foreground process matches an allow-listed executable/argument pattern.
- The working directory matches or is explicitly allowed to differ.
- The terminal still appears rate-limited, blocked, or idle in an expected state.
- There is no approval, authentication, upgrade, destructive-operation, or user-choice prompt.
- The rate-limit evidence is not merely stale scrollback beneath newer work.

If any critical check fails, do not inject input.

### Exactly-once behavior

True distributed exactly-once delivery is unnecessary, but the tool must make duplicate submission unlikely:

1. Persist RESUMING before sending input.
2. Include an attempt ID and timestamp in state.
3. Send one structured action.
4. Read output and confirm a state transition.
5. Persist success or failure.
6. On watcher restart during an uncertain send, require evidence before retrying.

A duplicate `continue` can alter an interactive prompt, so uncertain cases should prefer manual review.

────────

## 11. Configuration

Suggested file:

```text
~/.config/herdr-auto-resume/config.yaml
```

Example:

```yaml
version: 1

runtime:
  type: herdr
  transport: socket
  reconnect_delay: 2s
  polling_fallback: 10s

monitoring:
  default_enabled: false
  discover_interval: 15s
  recent_lines: 100
  max_wait_horizon: 192h

resume:
  margin: 60s
  max_attempts_per_event: 1
  verification_timeout: 90s
  fallback_wait: 5h

providers:
  claude:
    enabled: true
    prompt: "Continue where you stopped. Re-check the current project state first."
  codex:
    enabled: true
    prompt: "Continue the previous task from where you stopped. First inspect the current repository state and existing progress before making further changes."

notifications:
  on_limit: true
  on_resume: true
  on_failure: true

logging:
  level: info
  file: "~/.local/state/herdr-auto-resume/auto-resume.log"
```

Configuration must be validated at startup. Invalid values should produce a clear error rather than silently creating unsafe defaults.

────────

## 12. CLI and user experience

Proposed commands:

```bash
# Run the watcher in the foreground
herdr-auto-resume run

# Run against a specific Herdr pane
herdr-auto-resume enable w1:p1

# Enable all currently recognized agents in a workspace
herdr-auto-resume enable --workspace w1

# Disable monitoring
herdr-auto-resume disable w1:p1

# Show agents and pending jobs
herdr-auto-resume status

# Show one job and its evidence
herdr-auto-resume inspect <job-id>

# Cancel a pending resume
herdr-auto-resume cancel <job-id>

# Validate Herdr connectivity, schema, paths, hooks, and providers
herdr-auto-resume doctor

# Run detector fixtures against supplied text without sending input
herdr-auto-resume detect --provider claude --file fixture.txt

# Force a safe simulated event for end-to-end testing
herdr-auto-resume test --pane w1:p1 --reset-in 2m --dry-run
```

### Optional TUI

The original autoclaude Bubble Tea interface can be retained after transport refactoring. A Herdr-oriented TUI could display:

- Workspace and pane label.
- Detected provider.
- Agent status.
- Auto-resume enabled/disabled.
- Limit state.
- Reset time in local time and UTC.
- Time remaining.
- Validation errors.
- Last resume result.

The daemon and CLI are more important than the TUI for the MVP.

### Notifications

Use Herdr notifications for meaningful state changes:

- Usage limit detected.
- Resume scheduled.
- Resume submitted.
- Agent became active again.
- Validation failed.
- Manual intervention required.

Do not notify on every polling cycle.

────────

## 13. Suggested repository structure

```text
herdr-auto-resume/
├── cmd/
│   └── herdr-auto-resume/
│       └── main.go
├── internal/
│   ├── app/
│   │   ├── app.go
│   │   └── reconcile.go
│   ├── config/
│   │   ├── config.go
│   │   └── validation.go
│   ├── coordinator/
│   │   ├── coordinator.go
│   │   ├── scheduler.go
│   │   └── state_machine.go
│   ├── runtime/
│   │   ├── runtime.go
│   │   ├── models.go
│   │   ├── herdr/
│   │   │   ├── cli.go
│   │   │   ├── socket.go
│   │   │   ├── events.go
│   │   │   └── schema.go
│   │   └── tmux/
│   │       └── adapter.go
│   ├── provider/
│   │   ├── provider.go
│   │   ├── registry.go
│   │   ├── claude/
│   │   │   ├── provider.go
│   │   │   ├── detection.go
│   │   │   ├── reset_parser.go
│   │   │   └── actions.go
│   │   └── codex/
│   │       ├── provider.go
│   │       ├── detection.go
│   │       ├── reset_parser.go
│   │       └── actions.go
│   ├── terminal/
│   │   ├── normalize.go
│   │   └── tail.go
│   ├── store/
│   │   ├── store.go
│   │   └── json_store.go
│   ├── notify/
│   │   └── notify.go
│   ├── tui/
│   │   └── ...
│   └── testutil/
│       ├── fake_clock.go
│       ├── fake_runtime.go
│       └── fixtures.go
├── testdata/
│   ├── claude/
│   ├── codex/
│   └── terminal/
├── docs/
│   ├── architecture.md
│   ├── detection-fixtures.md
│   ├── herdr-api.md
│   └── troubleshooting.md
├── BRIEF.md
├── LICENSE
├── NOTICE
├── README.md
└── go.mod
```

────────

## 14. Implementation phases

### Phase 0 — Repository and evidence collection

- Fork henryaj/autoclaude.
- Preserve MIT license and attribution.
- Record the initial upstream commit.
- Capture real Claude and Codex usage-limit screens as sanitized fixtures.
- Record installed versions of Herdr, Claude Code, and Codex used for testing.
- Export the Herdr API schema from the target Herdr release.

Exit criteria: repository builds; fixture policy and compatibility targets are documented.

### Phase 1 — Transport abstraction

- Create the runtime interface.
- Move current tmux calls behind a tmux adapter.
- Remove direct tmux validation from main.go.
- Make the existing Claude auto-resume logic pass through the abstraction.
- Add a fake runtime for tests.

Exit criteria: existing tmux behavior still works through the new interface; core packages do not import tmux.

### Phase 2 — Herdr CLI MVP

- Implement pane and agent discovery through Herdr CLI wrappers.
- Implement recent output reads.
- Implement process inspection.
- Implement structured text/key submission.
- Add doctor and dry-run modes.
- Support manual per-pane enablement.

Exit criteria: a simulated limit in a Herdr pane is detected and a dry-run resume is scheduled.

### Phase 3 — Persistent scheduler and safety gates

- Add the state machine.
- Add atomic JSON persistence.
- Add fake-clock tests.
- Reconcile pending jobs after watcher restart.
- Add identity and foreground-process validation.
- Add exactly-once protections.

Exit criteria: a watcher can be stopped while waiting, restarted, and safely resume the same simulated session once.

### Phase 4 — Claude Code production support

- Expand reset parsing.
- Add timezone and daylight-saving tests.
- Add live-tail/chrome-aware detection.
- Handle clear wait/stop menus conservatively.
- Add notifications and logs.
- Test detached and over SSH.

Exit criteria: all known Claude fixtures pass, negative fixtures do not trigger, and an end-to-end simulated reset resumes successfully.

### Phase 5 — Codex support

- Collect and classify Codex fixtures.
- Integrate available lifecycle hooks and transcript signals.
- Implement Codex-specific validation and resume action.
- Verify session continuity after a real or simulated limit.

Exit criteria: Codex limit detection and safe resume work independently of Claude logic.

### Phase 6 — Herdr socket client

- Implement socket schema negotiation/version checks.
- Bootstrap with `session.snapshot`.
- Subscribe to events.
- Add reconnect and cache reconciliation.
- Retain polling fallback.

Exit criteria: normal monitoring uses the event-driven socket client and survives Herdr client detach/reattach and watcher reconnects.

### Phase 7 — Packaging and optional TUI/plugin

- Build release binaries.
- Add systemd user service and launchd examples.
- Rework the Bubble Tea TUI for Herdr workspaces and panes.
- Evaluate a Herdr plugin package.

Exit criteria: documented installation and upgrade path; no manual process management required for normal use.

────────

## 15. Testing strategy

### Unit tests

- All known reset-message formats.
- Timezone parsing.
- DST forward and backward transitions.
- Clock time that crosses midnight.
- Weekly reset dates.
- Relative duration parsing.
- Invalid and implausible reset times.
- ANSI/control-sequence normalization.
- Tail extraction.
- State-machine transitions.
- Atomic persistence and recovery.
- Duplicate-resume prevention.
- Provider process classification.

Use a fake clock; tests must not sleep for real time.

### Negative detection fixtures

Include examples where rate-limit text appears in:

- Source code.
- A README.
- A prompt written by the user.
- Agent analysis of the auto-resume feature.
- Command history.
- Old scrollback with newer successful output below it.
- Test output.
- A different pane not running the expected provider.

None of these should schedule a resume.

### Integration tests

- Fake Herdr CLI responses.
- Local fake Unix socket server implementing required methods.
- Socket reconnect and snapshot reconciliation.
- Pane closes while waiting.
- Agent process is replaced while waiting.
- Pane moves while waiting.
- Herdr server restarts while waiting.
- Watcher restarts before and during a resume attempt.
- Two panes reach limits with different reset times.
- Same pane emits the same limit message repeatedly.

### End-to-end tests

Provide a test-pattern mode similar to autoclaude:

1. Start a harmless interactive fixture process in a Herdr pane.
2. Render a provider-specific simulated limit message.
3. Schedule reset a few minutes ahead.
4. Detach from Herdr.
5. Confirm the watcher waits.
6. Confirm exactly one continuation message is delivered.
7. Confirm state returns to monitoring.

Real provider-limit testing should be supplemental, not required in CI.

────────

## 16. Safety and security requirements

- Communicate only through Herdr's local CLI or local Unix socket.
- Do not expose a network listener by default.
- Do not accept arbitrary commands in configuration.
- Treat all pane output as untrusted text.
- Never interpolate pane output into a shell command.
- Use argument arrays rather than shell command strings where possible.
- Restrict automatic input to provider-specific allow-listed actions.
- Never auto-select purchases, upgrades, extra usage, permissions, or destructive confirmations.
- Redact terminal evidence in logs where it might contain secrets.
- Default to disabled until explicitly enabled.
- Provide a global emergency disable switch.
- Set a maximum automatic attempt count.
- Stop after ambiguous identity, session, or UI changes.
- Make dry-run mode easy and prominent.

Suggested emergency controls:

```bash
herdr-auto-resume disable --all
herdr-auto-resume run --dry-run
```

────────

## 17. Observability

Each resume event should produce structured logs containing:

- Event/job ID.
- Provider.
- Workspace/tab/pane IDs.
- Agent/session identity where safe.
- State transition.
- Detection confidence.
- Parsed reset time and timezone.
- Validation outcome.
- Resume attempt number.
- Result and elapsed time.

Do not log full project prompts or terminal scrollback by default. Store only the matched and sanitized evidence required for diagnosis.

Suggested status output:

```text
PANE    PROVIDER  MODE  STATE         RESET                RESULT
w1:p1   claude    auto  waiting       2026-07-31 03:00 CEST —
w2:p3   codex     auto  monitoring    —                    resumed 1x
w3:p2   claude    off   disabled      —                    —
```

────────

## 18. Compatibility policy

Coding-agent terminal UIs and messages change frequently. Compatibility should therefore be explicit:

- Record tested Herdr, Claude Code, and Codex versions in releases.
- Maintain fixtures by version when output formats differ.
- Keep custom detection patterns configurable.
- Fail conservatively when an unknown menu or UI layout is encountered.
- Detect Herdr protocol/schema changes at startup.
- Include doctor output in bug reports.
- Avoid depending on undocumented internal files when supported hooks, transcripts, or Herdr APIs are available.

The project should use semantic versioning. Detection-only compatibility fixes may be patch releases; new provider behavior or configuration changes should be minor releases.

────────

## 19. Project continuity guidance

Automatic terminal resumption does not guarantee that the agent retains complete knowledge of a long-running project. The recommended workflow should encourage the agent to maintain durable project state, for example:

```text
TASKS.md
PROGRESS.md
DECISIONS.md
```

Before a long unattended run, the user can instruct the agent to:

- Update progress after each meaningful milestone.
- Record unresolved issues and the next action.
- Commit safe checkpoints to Git.
- Inspect git status, diffs, and project notes after resuming.
- Avoid leaving the repository in an intentionally broken state for extended periods.

The default continuation prompt should explicitly tell the agent to inspect current repository state before proceeding.

────────

## 20. Acceptance criteria for the MVP

The MVP is complete when:

1. It runs on an Ubuntu host with Herdr, Claude Code, and Codex installed.
2. It discovers recognized agents in Herdr panes.
3. Monitoring is opt-in and can be enabled or disabled per pane.
4. It detects all committed positive fixtures for Claude and Codex.
5. It does not trigger on committed negative fixtures.
6. It parses and stores reset times in UTC while displaying local time correctly.
7. It persists waiting jobs across watcher restarts.
8. It safely handles a Herdr disconnect and reconnect.
9. It verifies the original provider/process/session before input injection.
10. It refuses to act on upgrade, permission, authentication, or ambiguous menus.
11. It submits only one continuation action for one detected limit event.
12. It verifies that the agent state or output changed after submission.
13. It provides status, inspect, cancel, doctor, and dry-run commands.
14. It writes useful logs without storing full terminal transcripts by default.
15. It has unit tests for parsing, state transitions, persistence, and duplicate prevention.
16. It has an automated Herdr simulation test requiring no real quota exhaustion.
17. It includes license attribution for all reused MIT-licensed source material.

────────

## 21. Open questions and required experiments

1. **Codex limit output:** What exact terminal and transcript events are produced by the currently installed Codex version for five-hour and weekly limits?
2. **Codex continuation:** Does plain `continue` reliably resume the interrupted interactive session, or should the tool use a longer prompt or a native resume command?
3. **Claude hooks:** Can a stable Claude lifecycle hook identify usage-limit stops reliably enough to reduce terminal scraping, while still retaining output verification?
4. **Herdr events:** Which Herdr events are emitted for pane output changes and agent state changes in the target version, and is `pane.wait_for_output` sufficient for the first socket implementation?
5. **Durable identity:** Which Herdr agent/session fields remain stable through pane moves and server restore?
6. **Plugin versus daemon:** Does a Herdr plugin receive all required startup and event capabilities without making releases too dependent on Herdr plugin API changes?
7. **Menu handling:** Should the MVP manipulate Claude's wait/upgrade menu, or should it require the agent to already be in the final blocked state?
8. **Weekly limits:** Should waits longer than a configurable duration require user confirmation even when the reset time is valid?
9. **Multiple limits:** After resuming, should the pane remain armed automatically for later limits, or should one-shot mode be the default?
10. **Notifications:** Is Herdr notification delivery sufficient on a headless VPS, or should optional webhook/email integrations be considered later?

Recommended MVP decisions:

- Plain blocked-state handling first; menu navigation second.
- One automatic attempt per event.
- Continuous monitoring remains enabled after a successful resume, but is always explicit opt-in.
- Maximum wait horizon of eight days.
- Standalone daemon before plugin packaging.
- Herdr CLI adapter before raw socket adapter.

────────

## 22. Upstream sources and references

Verified while preparing this brief on 2026-07-30:

- Herdr repository: https://github.com/herdrdev/herdr
- Herdr Socket API documentation: https://herdr.dev/docs/socket-api/
- autoclaude: https://github.com/henryaj/autoclaude
- claude-auto-retry: https://github.com/cheapestinference/claude-auto-retry
- ccgram: https://github.com/alexei-led/ccgram
- ccgram provider documentation: https://github.com/alexei-led/ccgram/blob/main/docs/providers.md

### Key source observations

- autoclaude polls tmux panes, detects Claude Code limit messages, parses the reset time, and sends a continuation sequence after reset. Its repository separates detection, tmux, and TUI packages, making it a manageable refactoring base.
- claude-auto-retry demonstrates hardened reset-time parsing, tail-focused detection, foreground-process validation, safety margins, bounded retries, overload handling, and broad Claude message coverage.
- ccgram demonstrates a transport-neutral multiplexer boundary with concrete tmux and Herdr backends, plus separate provider support for Claude Code and Codex.
- Herdr exposes pane reads, process information, text/key injection, agent operations, event subscriptions, notifications, schema export, and session snapshots through its local API.
