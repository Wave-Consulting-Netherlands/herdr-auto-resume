# PROGRESS

## Upstream provenance

Fork of [`henryaj/autoclaude`](https://github.com/henryaj/autoclaude) at commit
`39ad5ef1818a9c71241bea463da3af33f1dccf69` ("Handle SIGINT/SIGTERM for clean ctrl-c exit",
branch `master`, tagged locally as `upstream-39ad5ef`).

Upstream is MIT licensed, "Copyright (c) 2025 Henry Stanley". The `LICENSE` file is
preserved unmodified. This fork retains upstream attribution per BRIEF.md §6.4 and §20.17.

Fork remote: `https://github.com/Wave-Consulting-Netherlands/herdr-auto-resume` (origin);
`https://github.com/henryaj/autoclaude` (upstream).

## Completed

- **Phase 0 (in progress)** — 2026-07-30
  - Go 1.26.5 installed to `~/.local/go` (linux-arm64; host had no Go toolchain).
  - GitHub fork created: `Wave-Consulting-Netherlands/herdr-auto-resume`.
  - Repo initialized in `/home/ubuntu/dev/Herdr-auto-resume`; `master` checked out from
    `upstream/master` at `39ad5ef`; upstream tests green on import
    (`go build ./...` + `go test ./...`, detection package 55 assertions).
  - Branch `phase-0-bootstrap`: committed `BRIEF.md`, this file, README fork notice.
- **Phase 1 complete** — 2026-07-30
  - Extracted the tmux-independent runtime interface, geometry, fake runtime, and resume
    coordinator; moved tmux behind its runtime adapter; rewired the TUI and CLI dry-run
    support.
- **Phase 2 complete** — 2026-07-30
  - Added CLI subcommand dispatch, the fixture-driven Herdr CLI runtime adapter, headless
    run loop/command, and ordered doctor diagnostics.
- **Phase 3 code complete** — 2026-07-31
  - Added atomic persistent JSON jobs, coordinator sink/post-poll seams, validation safety
    gates, exactly-once resume persistence and verification, restart reconciliation, mtime
    reload for external cancel, CLI status/inspect/cancel, and run-state wiring.
  - Commit 7 live Herdr E2E remains intentionally with the orchestrator; no real herdr
    binary was run in this implementation session.

## Design decisions

- **Module rename in Phase 0** to `github.com/Wave-Consulting-Netherlands/herdr-auto-resume`, before any
  new packages exist, so the rename touches only `go.mod` + 3 import sites instead of every
  later file. Note: `gh auth status` displays the account alias `wave-consulting-nl`, but
  the actual GitHub login (and thus module path owner) is `walt-verweij`.
  **Superseded 2026-07-31:** repo transferred to the `Wave-Consulting-Netherlands` org and
  the module renamed to `github.com/Wave-Consulting-Netherlands/herdr-auto-resume` (exact
  GitHub casing, so the module path always matches the canonical repo URL). GitHub's
  transfer dropped the fork linkage to `henryaj/autoclaude`; attribution remains in
  LICENSE/README and the `upstream` remote.
- `.goreleaser.yml` / CI workflows left untouched this phase; the goreleaser binary name
  still says `autoclaude`. To be revisited in the packaging phase (BRIEF.md Phase 7).
- Scope of current work order: BRIEF.md Phases 0–3 code (bootstrap, runtime abstraction,
  Herdr CLI adapter, persistence, and safety-gated scheduler). No Codex provider, socket
  client, or plugin; commit 7 live E2E remains pending.
- Runtime interface intentionally has no `Subscribe` method; Phase 1 remains polling-based
  and keeps the coordinator independent of any concrete adapter.
- `SendText` delegates to plain tmux `send-keys` without `-l`, preserving upstream behavior.
- The tmux adapter ignores `ReadPane`'s `lines` argument because upstream captures the
  visible viewport.
- Window pinning moved into `tmux.New`, which calls `CurrentWindowID` once at startup.
- Runtime pane descriptors are separate from coordinator-owned pane state.
- Herdr child processes scrub every `HERDR_*` variable inherited from the parent; only an
  explicitly configured `SocketPath` is re-added. This avoids the inherited socket hazard,
  where a child could accidentally target the controller's own Herdr session.
- Headless `run` requires strict `--pane` opt-in. This prefers false negatives over
  accidentally sending input to an unselected pane.
- Herdr pane reads use `--source recent` and consume plain text directly; pane reads do not
  decode the JSON envelope used by the other CLI commands.
- Herdr command failures first decode the JSON error envelope and otherwise preserve the
  command failure for callers to report.

### Phase 3 design decisions

- **D1:** Persistence lives in `internal/store`; lifecycle and safety gates live in
  `internal/jobs`; coordinator owns only its small sink interface and core packages do not
  import concrete adapters or jobs/store.
- **D2:** Known-reset headless episodes belong to the job store; unknown-reset and test-pattern
  paths remain coordinator-owned; no-sink behavior remains the upstream path.
- **D3:** The sink is level-triggered and deduplicates by pane while active; priming order is
  unchanged and a resumed pane remains armed for later evidence.
- **D4:** The manager persists `RESUMING` before using the shared Escape/continue/Enter helper.
- **D5:** Validation rereads the tail and requires Claude identity plus a rate-limit or safe
  idle prompt, rejecting menu selectors and unsafe fingerprints.
- **D6:** Verification succeeds on a cleared limit or changed evidence hash before the strict
  deadline; dry-run records `RESUMED` without pane input.
- **D7:** Jobs beyond the configured horizon are durably `FAILED` while retaining ownership.
- **D8:** Tick/HandleLimit mtime reloads let external terminal edits such as cancel win.

## Test results

- 2026-07-30, upstream import at `39ad5ef`: `go build ./...` OK; `go test ./...` — all
  detection tests pass (only package with tests upstream). Toolchain go1.26.5 linux/arm64.
- 2026-07-30, Phase 1 final `go test ./...`:

  ```text
  ?    github.com/Wave-Consulting-Netherlands/herdr-auto-resume [no test files]
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/arch (cached)
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/coordinator (cached)
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection (cached)
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime (cached)
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/tmux (cached)
  ?    github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/tui [no test files]
  ```

- 2026-07-30, Phase 2 final `go test ./...`:

  ```text
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume (cached)
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/arch (cached)
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/coordinator (cached)
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection (cached)
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime (cached)
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/herdr (cached)
  ok   github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/tmux (cached)
  ?    github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/tui [no test files]
  ```

- 2026-07-30, Phase 2 live E2E on this host (herdr 0.7.5, protocol 17, server running):
  - `herdr-auto-resume doctor` → all 6 checks PASS (binary, socket, status/protocol,
    adapter round-trip decoded 6 panes, schema JSON, self-pane `wA:p1` detected with
    self-exclusion).
  - Dry-run: scratch workspace `wB`, pane staged with the Claude-prompt fixture +
    `PING-E2E-MARKER`; `run --pane wB:p1 --dry-run --test-pattern PING-E2E-MARKER
    --interval 1s` → exactly one `DRY-RUN` action line, zero input delivered to the pane.
  - Live send: same setup without `--dry-run` → exactly one action; pane received
    escape → "continue" → enter. (In the zsh scratch pane the leading `c` was consumed
    by vi-mode Escape handling — an artifact of testing against a shell; a real Claude
    pane consumes Escape harmlessly. Upstream tmux behavior is identical.)
  - Latch verified: no duplicate sends across subsequent polls; clean exit on SIGTERM.
  - Scratch workspace closed after the test.

- 2026-07-31, Phase 3 final code gate:

  ```text
  ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume	1.097s
  ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/arch	1.023s
  ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/coordinator	1.236s
  ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection	1.031s
  ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/jobs	1.874s
  ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime	1.027s
  ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/herdr	1.023s
  ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/tmux	1.015s
  ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store	1.052s
  ?   	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/tui	[no test files]
  ```

- 2026-07-31, Phase 3 live E2E on this host (herdr 0.7.5, scratch workspace `wC`,
  `/bin/cat` fixture pane, banner "You've hit your limit · resets Nm"):
  - **Detection-source fix found live:** `pane read --source recent` covers only
    scrollback and is empty on fresh/quiet panes; adapter default switched to
    `--source detection` (includes viewport). Commit `b9397cb`.
  - Dry-run restart drill: watcher A created job → WAITING persisted → killed mid-wait;
    watcher B reconciled, waited past reset+margin, exactly one dry-run resume →
    RESUMED (attempts=1), zero input delivered.
  - RESUMING-at-restart drill: job hand-set to RESUMING; restart → MANUAL_REQUIRED
    ("watcher restarted during uncertain resume send"), zero sends.
  - Live pass: restart mid-WAITING → single real esc→"continue"→enter delivered after
    reset+margin → VERIFYING_RESUME → RESUMED (attempts=1). (Raw cat pane rendered
    ESC+c as terminal reset — harness artifact only.)
  - Cancel under running watcher (D8): new job cancelled from a second shell → job
    CANCELLED (attempts=0), watcher honored it, no sends.
  - Conservative behaviors observed and accepted: (1) a `❯` anywhere in the read tail
    fails validation gate 9 → MANUAL_REQUIRED — protects real Claude rate-limit menus;
    means panes with starship-style prompts in the tail window park safely instead of
    resuming. (2) After RESUMED, identical evidence hash suppresses a new job (prevents
    resend loops when the banner never cleared); a genuinely new limit re-arms because
    its reset text differs. (3) Any non-RESUMED terminal job parks the pane until the
    state file is cleaned — no `clear`/`ack` command yet (see BACKLOG.md).

## Phase 4 code complete — 2026-07-31

- Added the stdlib-only terminal normalization leaf and explicit fake-clock detection
  seam; legacy `CheckRateLimit`/`HasReset` wrappers and the TUI path remain intact.
- Added typed reset parsing for local/IANA/abbreviation clocks, relative and weekly
  date-time resets, DST gap/fold handling, horizon rejection, and additive store fields.
- Added Claude message families, versioned positive fixtures, chrome-aware live-tail
  detection, quote/tool-echo/stale guards, and the unheaded `⎿` live-banner exception.
- `Analyze.Actionable` now gates scheduling and periodic/legacy sends; menus remain
  visible but block sending at validation gate 9. Evidence hashing remains raw-content
  SHA-256 and schema version remains 1.
- Added the read-only `detect --file` diagnostic with UTC/local reset output. The live
  E2E drills in PLANS.md commit 7 remain with the orchestrator.

### Final code gate

```text
ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume	1.086s
ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/arch	1.029s
ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/coordinator	1.191s
ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection	1.100s
ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/jobs	1.944s
ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime	1.018s
ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/herdr	1.023s
ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/tmux	1.014s
ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store	1.053s
ok  	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/terminal	1.016s
?   	github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/tui	[no test files]
```

## Next task

Phase 4 live E2E drills (PLANS.md commit 7), then BRIEF.md Phase 5 (Codex provider)
