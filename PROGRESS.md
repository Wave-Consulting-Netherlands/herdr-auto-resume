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

- 2026-07-31, Phase 4 review + live E2E drills (herdr 0.7.5, scratch workspaces wE/wF):
  - **Review fix:** `detect` smoke against the menu fixture exposed two masking defects —
    boxed menus (`│ ❯ 1. …│`) were invisible to menu detection (validation could have
    sent Enter into a menu), and the menu box below a banner killed actionability (no job
    for the most common real limit screen). Fixed in `167fa4f` (border-tolerant menu
    matching; menu blocks are non-content for the stale guard) with regression tests.
  - Weekly drill: banner "resets Friday 7:34am" → job kind=date-time confidence=medium,
    resume 07:34:30Z (reset+30s margin), full cycle → RESUMED attempts=1.
  - Timezone drill: banner "resets 9:39am (Europe/Amsterdam)" on a UTC host → resolved
    07:39:00Z exactly (kind=absolute, tz=Europe/Amsterdam, confidence=high), full cycle
    → RESUMED attempts=1. Confirms the printed-timezone bug fix end-to-end.
  - Negative drill: pane cat-ing BRIEF-style prose quoting limit messages (incl.
    unquoted "…limit · resets 3pm" prose at tail) watched 3 min → zero jobs, state file
    never created.
  - Detached/SSH: the production watcher (wD:p1) has run detached across SSH sessions
    since Phase 3 deployment.
  - Final gate after fix: 239 tests green incl -race.

## Phase 5 code complete — 2026-07-31

- Added a provider interface and registry with authoritative `Pane.Agent` hint-wins
  resolution; empty hints require exactly one content match, and ambiguity fails closed.
- Preserved Claude detection, gate-9 safety behavior, and Escape → `continue` → Enter
  sequencing with the injected 100 ms sleep; nil registries retain Claude-only defaults.
- Added evidence-based Codex 0.146/0.144 banner families, reset-tail normalization, live
  chrome/quote/stale/busy guards, no-menu semantics, Codex prompt → Enter resume, and no
  periodic nudge.
- Made coordinator polling and persistent jobs provider-aware without changing the store
  schema. Unknown provider state becomes MANUAL_REQUIRED; schema-1 empty-provider state
  falls back to Claude compatibility.

### Final code gate

```text
go build ./...                              OK
go vet ./...                                OK
go test ./... -race -count=1                OK
```

All packages passed, including `internal/provider`, `internal/provider/claude`, and
`internal/provider/codex`; the gate ran with Go 1.26.5 and `GOCACHE=/tmp/herdr-go-cache`.

## Phase 5.5 complete — 2026-07-31

- Remediated all ten review findings in four commits: transactional sidecar flock
  locking and authoritative cancellation; fail-stop sends with honest failure
  propagation; current-pane provider identity and non-live-evidence verification;
  normalized episode fingerprints; immediate startup acquisition; split short
  detection/status cadence; timeout/config validation; protocol-aware doctor
  warnings; explicit zero margin; and impossible-date rejection.
- Store schema remains unchanged. syscall.Flock support is documented for the
  Linux/macOS target platforms.
- Semantic changes: fail-stop sends, non-live-evidence verification, episode
  fingerprinting, and split cadence. Review regression tests are permanent and
  the full race-enabled gate is green.

- 2026-07-31, Phase 5 + 5.5 live drills (herdr 0.7.5, scratch workspaces wG/wH/wJ):
  - **Review fix during drills:** wrapped 80-col banner lost its reset tail →
    continuation-line join in the codex analyzer (`5e10f17`), pinned by a live-captured
    fixture.
  - Codex full cycle (Pro-upgrade + model-switch banner variants): provider=codex,
    kind=local-clock/high, restart mid-WAITING, exactly one long-prompt send with NO
    escape, RESUMED attempts=1 — twice (pre- and post-remediation).
  - Hint-wins negative on real codex pane w7:p1 (3 min): zero jobs. Ambiguity negative
    (claude banner + codex chrome, no agent label): zero jobs. Deployed schema-1 state
    reads clean with PROVIDER column.
  - Cancel-under-running-watcher (R1 live): job stayed CANCELLED, attempts=0, zero
    sends.
  - review.md triage (BACKLOG 8): all ten findings validated, none false; remediated
    as R1–R4 on this branch. The live-miss incident (BACKLOG 9) is mitigated by the
    immediate post-enable poll + 30s-capped detection ticker; full event-driven
    acquisition is Phase 6.

## Pre-Phase 6 handoff

BRIEF.md Phase 6 — Herdr socket client: session.snapshot bootstrap, event
subscriptions (pane output/agent state — completes the BACKLOG 9 fix), reconnect +
cache reconciliation, polling fallback retained.

## Phase 6 code complete — 2026-07-31

- Added a dial-per-request Herdr socket Runtime with id echo checks, bounded deadlines,
  nested pane-read decoding, ping/snapshot, environment isolation, and TerminalID pane
  identity (CLI and socket transports share the decode model).
- Added the neutral EventSource capability and long-lived subscription client with
  dot/underscore decoding, trigger coalescing, retained move/resync events, reconnect
  bootstrap, and cancellation joining. The run loop feeds the same detection channel as
  polling; CLI remains the default and polling remains unconditional fallback.
- Added flock-transactional TerminalID stamping, pane move reassignment, snapshot
  reconciliation, monitored terminal filtering, and socket-mode doctor checks.
- Decisions condensed: one request per ordinary connection; one event connection;
  socket never reads HERDR_*; schema remains version 1 with only Job.TerminalID added;
  `--session` is rejected with socket mode; real lifecycle/refire behavior remains live
  probe work.

### Final code gate

```text
go build ./...                              OK
go vet ./...                                OK
go test ./... -race -count=1                OK
```

All packages passed with Go 1.26.5, `PATH=$HOME/.local/go/bin:$PATH`, and
`GOCACHE=/tmp/herdr-go-cache` (`GOMODCACHE=/tmp/herdr-go-mod-cache` was required by
the sandbox's read-only default module cache).

- 2026-07-31, Phase 6 probes + live drills (herdr 0.7.5, socket transport):
  - **Review fixes during drills:** request ids must be JSON strings (numeric →
    invalid_request; doctor caught it live) — fixed in socket.go + events.go.
  - Probes: no output_matched refire within a subscription; new subscription re-matches
    current content (recycle = primary re-arm, design validated); envelope
    `{"event":dot-kind,"data"}` confirmed; subscribe replays historical lifecycle
    events (refresh-triggers only). Recorded in docs/herdr-api.md.
  - doctor --transport socket: all PASS (ping/protocol 17, snapshot 7 panes,
    subscription round-trip).
  - **BACKLOG-9 live acceptance:** clean-window 2s transient banner at --interval 10m →
    durable WAITING job within ~1s via events (×1); poisoned-window 40s banner → caught
    via the 30s ticker floor. Poisoned-window <30s transients degrade to the ticker —
    documented as BACKLOG 10 with a damped-recycle improvement sketch. BACKLOG 9 closed.
  - Pane-move drill: WAITING job followed wK:p1 → wP:p2 via terminal_id (source
    workspace closed entirely).
  - Negative: real codex pane w7:p1, socket transport, 2.5 min → zero jobs.
  - Live cycle under socket: banner → WAITING → exactly one resume → RESUMED attempts=1.
  - Soak: scratch socket-transport watcher started post-merge; wD:p1 stays on cli
    transport until the soak is clean.

## Phase 7 code complete — 2026-07-31

- Commits 1–5 implement release provenance, the single-instance run lock, strict YAML
  configuration, packaging assets, and the final documentation/conformance audit.
- D-P7-1: minimal read-only YAML, version 1, strict unknown-key rejection, config precedence
  built-in < file < explicitly set flags, and absent-default parity.
- D-P7-2: first release is v0.2.0; no prior upstream tags were changed.
- D-P7-3: Herdr plugin packaging is deferred; the capability inventory and wrapper sketch are
  in docs/packaging.md.
- D-P7-4: a Herdr-native TUI is deferred; the daemon/CLI and existing tmux TUI remain.
- D-P7-5: a non-blocking flock on the absolute state-file .run sidecar is held for the watcher
  lifetime; off disables it, and the transactional .lock remains separate.
- D-P7-6: status reset rendering now receives time.Local; Europe/Amsterdam regression coverage
  closes BACKLOG 2 without changing the store schema.
- D-P7-7: systemd user service and launchd example provide the two first-class run modes;
  linger is mandatory on headless hosts.

### BRIEF §20 conformance audit

1. Met — runs against Ubuntu with Herdr, Claude Code, and Codex integration points.
2. Met — Herdr runtime discovers recognized agents in selected panes.
3. Met with deviation — monitoring is strict opt-in; there are no runtime enable/disable verbs.
   Disable means restarting without the pane, intentionally preserving the safe headless model.
4. Met — committed positive Claude and Codex fixtures are covered.
5. Met — committed negative fixtures are covered.
6. Met — reset times persist in UTC and display through the caller-provided local timezone.
7. Met — waiting jobs persist across watcher restarts with schema 1.
8. Met — CLI and socket transports handle disconnect/reconnect paths.
9. Met — provider/process/session validation gates input injection.
10. Met — upgrade, permission, authentication, and ambiguous menus fail closed.
11. Met — persistence, fingerprints, and coordinator deduplication prevent duplicate actions.
12. Met — resume verification requires cleared evidence or changed output.
13. Met — status, inspect, cancel, doctor, and dry-run commands exist.
14. Met — logs are useful and do not store full terminal transcripts by default.
15. Met — unit and integration tests cover parsing, state, persistence, and duplicates.
16. Met — automated Herdr simulation tests require no real quota exhaustion.
17. Met — upstream MIT attribution remains in the unmodified LICENSE and fork notice.

Deferred by decision: a Herdr-native TUI; plugin packaging; a BRIEF §12 test subcommand
(the existing test-pattern and dry-run paths cover the need); and BRIEF §11 logging/notification
configuration sections.

### Final code gate

    go build ./...
    go vet ./...
    go test ./... -race -count=1

All five implementation commits pass the gate with Go 1.26.5, the requested PATH, and the
temporary Go caches. The systemd verifier emits the host's private-socket bind diagnostic
while exiting 0; plistlib parses the launchd example. Live drills, merge, CI release dry run,
and v0.2.0 release remain the orchestrator's commit-6 work.

- 2026-07-31, Phase 7 orchestrator verification (L1–L4 complete):
  - L1: `version` provenance renders; doctor (both transports) leads with version,
    config, and watcher-lock lines; all live checks PASS.
  - L2: run-lock drill — second instance on the same state file fails fast with holder
    PID + hint; different state file starts. The soak-vs-production footgun is closed.
  - L3: production config written (`~/.config/herdr-auto-resume/config.yaml`, doctor
    PASS); wD:p1 watcher restarted on `run --config … --transport cli`; wQ:p1 soak
    restarted on the same binary (socket, isolated state). Both healthy.
  - L4: merged to master, pushed. **BLOCKED: GitHub Actions has never fired on this
    repo (0 runs; repo-level enabled, workflows active) — org-level Actions policy
    suspected; needs org-admin enablement (or `gh auth refresh -s admin:org`).**
    Consequently L5 (v0.2.0 tag → CI release) is ON HOLD — do not tag until the
    release-dry-run job has gone green on master.
  - yaml.v3 integrity: `go mod verify` clean; go.sum hash matches the published
    v3.0.1 checksum (the sandbox's GOSUMDB=off fetch is retroactively verified).
  - Tested versions for the release record: herdr 0.7.5 (protocol 17), Claude Code
    2.1.220, codex-cli 0.146.0, Go 1.26.5.

- 2026-07-31, **v0.2.0 released** (L5–L6 complete):
  - Actions unblock: the repo's fork linkage kept workflows silently parked despite
    `enabled: true` everywhere; a repo-level Actions disable/enable toggle activated
    them. Three CI-only fixes followed: unix-socket path over the 108-byte sun_path
    limit in a doctor test (short MkdirTemp), pane-requirement tests not hermetic
    against a real user config (XDG_CONFIG_HOME isolation), and goreleaser v2's
    deprecated `archives.format` → `formats`.
  - CI green (tests + race + release-dry-run snapshot), tag `v0.2.0` via
    scripts/release.sh, Release workflow green: 4 tarballs + checksums.txt published.
  - linux_arm64 asset downloaded, checksum verified, `version` prints
    `0.2.0 (commit 5b172a1, built 2026-07-31T19:36:04Z, go1.23.12)`; installed to
    ~/.local/bin; both watchers (wD:p1 production cli+config, wQ:p1 soak socket)
    restarted on the released binary; doctor all-green.
  - Tested versions for this release: herdr 0.7.5 (protocol 17), Claude Code 2.1.220,
    codex-cli 0.146.0; CI toolchain go1.23.12 (go.mod), dev toolchain go1.26.5.

- 2026-08-01, soak restarted as a systemd user unit (BACKLOG 10 clock reset):
  - The pane-hosted socket soak died with its host workspace (wQ closed; pid 236620 gone).
    Restarted as `~/.config/systemd/user/herdr-auto-resume-soak.service` so a Herdr server
    bounce or a closed workspace can no longer end the soak: `--transport socket`,
    `--socket ~/.config/herdr/herdr.sock`, isolated `soak-state.json`, production config
    for the remaining settings, `Restart=on-failure`.
  - Soak panes are wR:p1 (codex) and wS:p1 (claude) — deliberately disjoint from the
    production watcher's panes, so two watchers can never resume the same pane.
  - Production config's pane list was stale (four panes from closed workspaces); trimmed to
    the live `[wA:p1]` and the service restarted. Verified `status: panes=1` for production
    and `panes=2` for the soak via throwaway `--state-file off --dry-run` instances;
    `doctor` (cli and socket) all PASS on both state files.
  - Note for the record: the production unit showed 166 restarts, all
    `list panes: ... ConnectionRefused` during a Herdr server bounce at 06:45 UTC. It
    self-healed; `Restart=on-failure` + `RestartSec=5s` behaved as designed.

- 2026-08-01, Phase 8 S1 steps 4–5: drill harness built and rehearsed (PLANS.md D-P8-2):
  - `scripts/soak-drill-harness.sh` idles as recognizable Claude chrome, emits a real limit
    banner on a trigger file with a reset ~2 minutes out, accepts one resume, then wipes the
    banner (ESC[3J, so scrollback goes too) and prints `DRILL-RESUMED-OK`.
  - Preflight: the banner detects as claude (`IsLimited=true`, `Actionable=true`,
    `MenuVisible=false`, high confidence) and matches none of Codex's identity patterns, so the
    agent-less pane resolves to exactly one provider instead of failing closed.
  - Two harness defects found by self-test before any live use: stdin EOF fell through the
    unguarded `read` and printed the success marker without a resume (fabricated evidence), and
    the periodic 15-minute nudge into an idle Claude pane would be consumed by the later
    blocking read. Fixed with a guarded read and a pre-read input drain.
  - **Product defect found (BACKLOG 12, scheduled as D-P8-9):** pane enablement is decided once
    at startup. Same pane, banner, binary, and transport — watcher started while the pane was
    unidentifiable ⇒ no job ever created (`panes=1`, inert); watcher started while the banner
    was already visible ⇒ job created and RESUMED within seconds. A boot-time systemd watcher
    that beats its agent panes to startup is silently inert.
  - Step 6: soak unit restarted with the drill pane appended (`--pane wR:p1 --pane wS:p1
    --pane wV:p1`), verified `panes=3` and doctor all-PASS. **48h evidence clock started
    2026-08-01 13:48:27 UTC; drill due 2026-08-03 13:48 UTC.**
  - Rehearsal PASS (the real T+48h shape): cold watcher start against the idle harness →
    trigger → WAITING job `4b606fe1` (reset 13:44:00Z, resume 13:45:00Z) → exactly one resume
    at 13:45:19Z, attempts=1 → verification saw cleared evidence → RESUMED. Socket transport,
    released v0.2.0 binary, throwaway state file. Rehearsal is explicitly not soak evidence.

- 2026-08-01, **real-world detection failure reported (BACKLOG 13, milestone SD):**
  - Two Claude sessions hit usage limits and were not resumed: `wA:p1` (production watcher,
    cli transport) and `wS:p1` (soak watcher, socket transport). Both panes were in their
    watcher's list at the time. User confirms a plain limit banner, no options menu.
  - Neither `state.json` nor `soak-state.json` has ever existed, so no job was created for
    either event — this is a detection/enablement failure, not a resume failure, and the tool
    has not been observed completing a real-world cycle in production, only in drills.
  - Ruled out: the menu fail-closed path (`MenuVisible=true` forces `Actionable=false`, which
    would have explained it, but the banner was plain); panes not being monitored.
  - Not determined: the failing path writes no job, no state, and no log line, and both panes'
    text is gone. Candidates left open in PLANS.md D3 rather than guessed at.
  - Actions: `scripts/limit-capture.sh` (414b8cc) running detached for ground truth; PLANS.md
    gains blocking milestone SD, plus D-P8-10 (silence is a defect), D-P8-11 (the flip is
    gated on a real resume, not drill evidence), D-P8-12 (instrument production only, leave
    the soak on v0.2.0).

- 2026-08-01, SD-D2 deployed to production (D-P8-10/D-P8-12):
  - Diagnostic logging landed as 20a109d (462 tests, -race): one line per evidence hash
    naming pane/provider/reason whenever a limited pane yields no job.
  - Built `0.2.0-diag (commit 20a109d)` to the DISTINCT path ~/.local/bin/herdr-auto-resume-diag
    and pointed only the production unit at it (unit comment says how to revert). The soak
    unit and its binary are untouched; soak pid 884025 still running since 13:48:27 UTC.
  - The next real limit on wA:p1 now either creates a job or names its reason in the journal,
    with scripts/limit-capture.sh holding the verbatim screen.

- 2026-08-02, SD-D3 diagnosis from evidence (Claude session JSONLs, prompted by comparing
  saaranshM/unsnooze):
  - Claude Code writes structured `"error":"rate_limit"` entries (429, banner text, sessionId,
    cwd, timestamp) into `~/.claude/projects/**/*.jsonl`. Ground truth for both 2026-08-01
    failures was on disk all along; the tool never looks there.
  - **Failure #1 SOLVED: never monitored.** 15:10:48Z, cwd psft_run_script, session ce7bb791,
    pane wW:p1 — in no watcher's pane list (production: [wA:p1]; soak: wR/wS/wV). Strict
    opt-in pane coverage plus ephemeral workspaces means new workspaces are unprotected by
    default. Coverage-model gap, not a code defect.
  - **Failure #2 NARROWED: swallowed after detection.** 15:14:56Z, wA:p1, session 829d1239
    ("resets 4:30pm (UTC)"). The production watcher was healthy the whole window (status
    ticks 15:00-16:30, 0 restarts) and the exact banner text parses Actionable=true at high
    confidence. So the drop happened between pane read and job creation: menu-visible
    fail-closed (the July fixture shows the modern UI auto-opens /rate-limit-options),
    not-auto (D-P8-9), a silent read error, or the banner outside the read window. v0.2.0
    logs none of these; the deployed diag build names each. Next occurrence is diagnosable
    by design.
  - **D-P8-6 spike answered early, and better than hoped:** `herdr pane list` exposes
    `agent_session` UUIDs for BOTH providers (wA→829d1239, wS→0bc1b8a7, wW→ce7bb791,
    wR→019fbc14 codex). Pane→session-file mapping is a herdr API lookup, no heuristics.
  - Strategic conclusion recorded in PLANS: a session-file/hook channel (unsnooze's model)
    would have caught both failures regardless of screen state, and session-identity resume
    (`claude --resume <id>`) removes the dead/unmonitored-pane class entirely.

- 2026-08-02, Phase A (session-file channel) LIVE on production (user-approved
  enable-before-drill):
  - D4.0-D4.3 merged (20e9dc2, 9ac216e, 064c8e2; 487 tests -race). Startup guards verified
    live: channel+--state-file off and channel+tmux both rejected with named reasons.
  - `providers.session_file_channel: true` added to the production config; unit restarted on
    `0.2.0-diag2 (064c8e2)`. Scanner sidecar lock present at state.json.scan.lock; soak
    untouched on v0.2.0.
  - Deviation from D-P8-18 recorded: an honest synthetic drill would require appending fake
    records to real Claude session files — refused. The Phase A live gate is therefore the
    next REAL limit observed through the enabled channel on production, which is also the
    D-P8-11 SD-closing evidence. Rollback = remove one config line.

- 2026-08-02, Phase B (targeted admission) implemented and LIVE on production:
  - D4.4 committed as b410e8f (503 tests, -race): monitoring.admit_session_matches, default
    off, requires session_file_channel + session-identity runtime + persistent state file;
    exact-one-pane match with agent/cwd consistency; per-episode admission logging;
    admission does not survive restart (fresh observation re-admits).
  - Production: config gained admit_session_matches: true (user-driven); unit restarted on
    0.2.0-diag3 (b410e8f); panes=4 verified, doctor PASS. Same enable-before-drill rationale
    as Phase A: the live gate is the next real limit in an unmonitored pane.
  - Coverage model now: the four configured Claude panes are watched statically; ANY new
    workspace whose session hits a limit is observed by the file channel and its pane
    admitted per-episode automatically. The soak stays isolated on v0.2.0.

- 2026-08-02, **SD CLOSED — first real-world resume wave (D-P8-11 gate passed 4×):**
  - ~10:00 UTC limit wave hit six Claude sessions simultaneously. Production watcher
    (0.2.0-diag3): wW, wS, wX, wY all RESUMED automatically with attempts=1 — the tool's
    first real end-to-end cycles. Jobs carried reset 10:00Z, resume 10:01Z, high confidence.
  - **Phase A and Phase B live gates both passed in the same event:** wZ:p1 (TencentDB) and
    w0:p1 (book-of-secret-knowledge) were workspaces created AFTER deployment, in no config;
    the session-file channel observed their limits and admission added them automatically
    (journal: "session-file admission: admitted pane=wZ:p1 …"). wS was admitted the same way
    (only in the soak's static list). Jobs were created for all six.
  - wZ and w0 popped the interactive menu → MANUAL_REQUIRED by design (Phase C not yet
    implemented). Menus answered manually (~14:17 UTC, Enter on "Stop and wait", cursor
    position re-verified before each send); both sessions returned to done/idle. Menu screens
    captured as Phase C fixtures — NEW variant: options are Stop and wait / Upgrade your
    plan / Upgrade to Team plan (no "extra usage").
  - Assessment: detection via session files, correlation, admission, scheduling, injection,
    and verification all worked on real limits. The remaining gap in the wave was exactly and
    only the menu — Phase C's case, now with real fixtures.

- 2026-08-02, Phase C (menu answering) LIVE on production: D4.5 committed as e207f35
  (525 tests, -race); `resume.answer_limit_menu: true` added to production config; unit
  restarted on 0.2.0-diag4 (e207f35); doctor PASS, panes=4. Same enable-before-drill
  rationale as A/B; live gate = the next real interactive limit menu. Had today's 10:00 UTC
  wave run with this enabled, all six sessions would have resumed with zero keystrokes.

- 2026-08-02, Phase D (`revive`) implemented AND live-drilled; three bugs the unit suite
  could not see, all found by driving the real system:
  - D4.6 landed as e894ea2; live drills then found (1) herdr reports the new pane as
    `root_pane`, not `workspace.panes` — the test had used a synthetic envelope; (2)
    `workspace create --cwd` does not set the pane shell's cwd AND `pane run` joins args
    through the shell unquoted, so `--resume` arrived valueless and opened the picker —
    fixed with a single-word launcher carrying shell-quoted values (105672c); (3) **a real
    lost-update race** (1774364): `Scan` read the sidecar once and rewrote that snapshot per
    file, erasing a concurrent process's write. Reproduced live — revive's ATTACHING intent
    was clobbered by the running watcher, so CompleteRevive found nothing. Every write was
    locked; the read was not inside the same lock. This is exactly what D-P8-21 specified and
    the implementation had missed. All sidecar mutations now go through mutate():
    lock -> fresh read -> apply -> write.
  - Note for future deploys: the race needs BOTH processes on the fixed build — the first
    re-drill still failed because the running watcher was the older binary.
  - Clean end-to-end drill after the fix: `revive a3df2d62` created workspace w18, attached a
    719k-token session from 1d 9h earlier, recorded ATTACHED{pane w18:p1} in the sidecar, and
    a second invocation correctly refused with the double-attach veto. 544 tests, -race.

- 2026-08-02, competitive review: `mo-arvan/herdr-claude-auto-retry` (herdr plugin registry,
  ~1500 lines JS, 76 tests, CI) does our core job. Source read, not just the README:
  - Their approach: scrape-only detection (broad regex battery incl. transient 429/5xx),
    per-pane monitor processes, registry lock files with pid-liveness + 60s staleness,
    recovery = `esc` then retry text then `enter`.
  - Ahead of us: herdr `[[events]]` hooks pick up new agent panes at creation (BACKLOG 15);
    transient-failure class with exponential backoff (BACKLOG 16); one-command plugin install
    with no daemon or config.
  - Behind us: blind `esc` menu dismissal with no read-back (we require question + cursor on
    "Stop and wait"); NO resume verification anywhere in the source; no durable job store, so
    a restart mid-wait loses the pending resume; scrape-only, so both of our 2026-08-01
    failure modes (limit never on screen, pane gone) remain open for them; Claude only.
  - Conclusion: reach vs correctness. Two gaps filed as BACKLOG 15/16, both additive. The
    plugin-packaging question (D-P7-3) is worth revisiting after v0.3.0 as a distribution
    decision — that registry is where users actually discover this class of tool.

## Project status

**All BRIEF.md phases 0–7 complete and released.** Remaining follow-ups live in
BACKLOG.md. Post-soak (24–48h clean on the socket soak unit, restarted 2026-08-01 06:56 UTC):
switch the production watcher to `--transport socket`.
