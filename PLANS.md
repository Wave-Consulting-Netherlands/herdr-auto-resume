# PLANS — Phase 3: Persistent scheduler and safety gates

Authoritative plan for BRIEF.md §14 Phase 3. Scope changes require updating this file.
Exit criteria: **a watcher can be stopped while waiting, restarted, and safely resume the
same simulated session exactly once.**

Out of scope: provider interface/registry, Codex support, socket client, weekly-reset
parsing expansion, menu navigation, TUI job views, `resume-now`/`enable-all`/`disable-all`.

Gate for every commit: `go build ./... && go vet ./... && go test ./... -race -count=1`
(Go at `~/.local/go/bin`, not on PATH).

## Design decisions

- **D1 Placement:** new `internal/store` (persistence) + `internal/jobs` (lifecycle/state
  machine). `jobs → coordinator, store, detection, runtime`; coordinator defines its own
  small sink interface and never imports jobs/store. Arch test extended: walk
  `../store` (commit 1) and `../jobs` (commit 3); forbid `internal/jobs`+`internal/store`
  imports from coordinator/detection/runtime.
- **D2 Authority split:** with a JobSink configured (headless herdr run only): known-reset
  limits → job store authoritative, coordinator's legacy send branch made inert by setting
  `ContinueSent=true` in the same Poll iteration ownership is taken. Unknown-reset limits →
  coordinator's 15-min periodic path stays authoritative, NOT persisted (nothing to
  schedule; restart re-detects losslessly). Test-pattern sends stay on the coordinator
  path, unpersisted. TUI/tmux path: no sink → byte-for-byte upstream behavior.
- **D3 Level-triggered idempotent sink:** `HandleLimit` called every poll while the
  condition holds (edge-trigger would miss pre-existing limits due to runcmd's
  Poll-then-EnableAll priming, which must NOT be reordered). Dedupe = at most one active
  job per pane ID (never by reset-time equality — "resets Nm" re-parses each tick and
  drifts). After RESUMED, a new limit creates a new job (stay-armed, BRIEF §21).
- **D4 The manager sends, not the coordinator:** persist RESUMING (fsync) BEFORE input.
  Shared `coordinator.SendContinueSequence(rt, paneID, sleep)` helper — one copy of
  esc→"continue"→enter.
- **D5 Terminal-state validation:** re-read tail; pass iff `IsClaudeCode` AND
  (`CheckRateLimit(...).IsLimited` OR new `detection.IsIdlePrompt` — prompt `^>` in last
  lines, no menu selector `❯` in tail). Else MANUAL_REQUIRED. Never use re-parsed
  ResetTime during validation (detection uses wall-clock internally).
- **D6 Verification:** success = `!IsLimited` OR sha256(tail) != evidence hash, before
  `VerifyDeadlineUTC` (default 90s) else FAILED. Dry-run skips verification (RESUMING→
  RESUMED directly, `DryRun:true`).
- **D7 Horizon:** reset further out than MaxHorizon (default 192h) → job created directly
  FAILED with clear error + notify; episode still owned (suppresses coordinator send).
- **D8 External cancel:** status/inspect/cancel edit the store file; manager checks file
  mtime each Tick and reloads; external terminal states (CANCELLED/DISABLED) win.

## Commits

1. **`internal/store`** — `store.go` (JobState consts WAITING/VALIDATING/RESUMING/
   VERIFYING_RESUME/RESUMED/MANUAL_REQUIRED/FAILED/CANCELLED/DISABLED/SESSION_GONE +
   `Terminal()`; `Job` struct: id, provider, pane_id, workspace, agent, proc_command,
   working_dir, detected_at, raw_reset, reset_at_utc, resume_at_utc, margin_secs, state,
   attempts, attempt_id, attempt_at_utc, verify_deadline_utc, last_validation, last_error,
   evidence_hash, evidence_at_utc, dry_run; `File{Version:1, Jobs}`; `Store` interface
   Load/Save/Path; `CorruptError{BackupPath,Err}`; `DefaultPath()` honoring
   XDG_STATE_HOME → `~/.local/state/herdr-auto-resume/state.json`).
   `json_store.go`: MkdirAll 0700, write `state.json.tmp.<pid>`, fsync file, rename,
   best-effort dir fsync; missing file → empty File; corrupt → backup
   `state.json.corrupt-<ts>` + empty + CorruptError (never crash-loop); unknown fields
   tolerated. Tests: round-trip, missing, corrupt+backup+recover, atomicity/tmp cleanup,
   XDG override (t.Setenv), perms 0700/0600. Arch test walks `../store`.
2. **Coordinator seams** — `LimitEvent{Pane, ResetsRaw, ResetTime(non-zero), Content,
   ObservedAt}`; `JobSink{HandleLimit(LimitEvent) bool}`; `WithJobSink`; `WithPostPoll(
   func(now time.Time))` invoked by RunLoop after each successful refresh+Poll;
   `SendContinueSequence` exported helper (sendContinue refactors onto it, dry-run stays
   in sendContinue). Poll change ONLY in the limited+ModeAuto known-reset branch per D2/D3.
   `detection.IsIdlePrompt` added (+tests). Tests: sink payload correctness; ownership
   suppresses legacy send; sink=false also no send (fail-safe); unknown-reset never calls
   sink (periodic path unaffected); ModeOff/TestPattern never call sink; no sink → all
   existing tests unchanged; postPoll fires once per tick with injected clock time.
3. **`internal/jobs` manager + gates** — `Config{Provider,Margin(60s),MaxHorizon(192h),
   VerifyTimeout(90s),ReadLines,DryRun}`; `New(rt, st, cfg, opts...)`; options WithClock/
   WithSleep/WithIDGenerator (default crypto/rand UUIDv4, no dep)/WithLogWriter.
   `HandleLimit`: dedupe per D3; horizon per D7; else create job (ProcessInfo fingerprint —
   errors leave fields empty which relaxes later checks; evidence sha256; ResetAtUTC;
   ResumeAtUTC=reset+margin; WAITING; persist; notify created/scheduled, log-only in
   dry-run). Persist failure → log + return false. `Tick(now)`: mtime merge (D8);
   WAITING→VALIDATING when now≥ResumeAtUTC; validate same tick.
   `validate.go` ordered gates: (1) not cancelled/disabled; (2) now≥ResumeAtUTC else wait;
   (3) Attempts==0 else MANUAL_REQUIRED; (4) ListPanes ok else stay VALIDATING (transient);
   (5) pane present else SESSION_GONE; (6) ReadPane ok + IsClaudeCode else MANUAL_REQUIRED;
   (7) proc command matches recorded (skip if empty) else MANUAL_REQUIRED; (8) cwd matches
   (skip if empty) else MANUAL_REQUIRED; (9) D5 terminal-state check else MANUAL_REQUIRED.
   Record LastValidation always; notify on MANUAL_REQUIRED/SESSION_GONE. Tests: transition
   table, horizon, dedupe (100 calls → 1 job), fingerprint/cwd mismatch, pane gone,
   transient runtime outage recovery, menu → MANUAL_REQUIRED, idle-prompt pass, not-Claude,
   zero-ResetTime contract, deterministic IDs, stay-armed second job. Arch test walks
   `../jobs`.
4. **Exactly-once + verification** — validation pass → persist RESUMING (AttemptID,
   AttemptAtUTC, Attempts=1); Save failure → do NOT send, revert to VALIDATING in memory;
   dry-run → RESUMED directly; real → SendContinueSequence; send error → MANUAL_REQUIRED;
   then persist VERIFYING_RESUME + VerifyDeadlineUTC; per-tick verify per D6; deadline →
   FAILED. Tests: failing-store spy proves zero sends; exactly one send across 50 ticks;
   spy store snapshot shows RESUMING persisted before send; verify via hash change; verify
   via !IsLimited; deadline FAILED at exact fake-clock instant; dry-run no runtime writes;
   send error → MANUAL_REQUIRED.
5. **Reconcile + E2E test** — `Reconcile()`: CorruptError → warn + continue; WAITING/
   VALIDATING kept; **RESUMING → MANUAL_REQUIRED** (uncertain send, never auto-retry);
   VERIFYING kept (expired deadline evaluated once next tick); WAITING with
   ResumeAtUTC−now>MaxHorizon → FAILED; terminal jobs kept for status, never ticked.
   E2E restart test through real wiring (fake runtime, "resets 5m" fixture content, temp
   state file, tick channel): instance A stops mid-WAITING (file shows WAITING, 0 sends);
   instance B reconciles, fake clock advanced past ResumeAtUTC **read back from the
   store**, exactly one send, content mutation → RESUMED; total sends across A+B == 1.
   Variant: file hand-set to RESUMING → MANUAL_REQUIRED, 0 sends.
6. **CLI wiring** — run flags: `--state-file` (default `auto`: DefaultPath for herdr,
   off for tmux; `off` disables; explicit path enables), `--margin` 60s, `--max-wait` 192h,
   `--verify-timeout` 90s. Wire store+manager+Reconcile+WithJobSink+WithPostPoll; keep
   SetPanes→Poll→EnableAll order unchanged; stderr line prints state path. New
   `jobscmd.go`: `status` (table: JOB(8-char) PANE STATE RESET(local) RESUME(UTC) ATTEMPTS
   ERROR), `inspect <id-prefix>` (full JSON, unique-prefix match), `cancel <id-prefix>`
   (active → CANCELLED, terminal → error). Tests: flag parsing incl auto/off/tmux-off,
   status golden, inspect prefix + ambiguity, cancel round-trip + rejection, dispatch.
   README + PROGRESS.md updates.
7. **Live E2E on this host** (run by orchestrator, not Codex): scratch workspace, `cat`
   fixture pane (Escape harmless, text echoes — avoids zsh vi-mode artifact), simulated
   limit "You've hit your limit · resets <T+3m>", dry-run pass with restart mid-wait +
   status/state.json checks, RESUMING-at-restart drill (→ MANUAL_REQUIRED, 0 sends), live
   pass (exactly one "continue" echoed; VERIFYING→RESUMED via hash change; notification),
   cancel honored by running watcher (D8). Record in PROGRESS.md.

## Risks

1. Latch gap: ownership must set ContinueSent=true in the SAME Poll iteration (D2).
2. Do not reorder runcmd priming (D3 handles it).
3. `detection` uses wall-clock internally — store parsed times once at detection; E2E
   tests read ResumeAtUTC back from the store before advancing fake clock. No clock
   injection into detection this phase.
4. Dedupe never by reset-time equality ("Nm" drift).
5. Persist-before-send lives in jobs (D4); failing-store test pins it.
6. Corrupt store never crash-loops.
7. Concurrent cancel needs D8 mtime reload.
8. Notifications only on job-created/submitted/active/MANUAL_REQUIRED/FAILED; dry-run
   suppresses.
9. Arch-test dir additions must match package creation order (store in c1, jobs in c3).
