# PLANS — Phase 5: Codex provider support

Supersedes the completed Phase 4 plan (git history / PROGRESS.md). Authoritative for
BRIEF.md §14 Phase 5. Exit criteria: **Codex limit detection and safe resume work
independently of Claude logic; Claude behavior byte-identical when provider=claude.**

Out of scope: transient-overload backoff (Codex `stream error … retrying N/M` recognized
only to REFUSE action), socket client, menu navigation, config file (D-P5-7), rollout-file
transcript integration (BACKLOG).

Gate per commit: `go build ./... && go vet ./... && go test ./... -race -count=1`
(Go at ~/.local/go/bin). Branch `phase-5-codex-provider`. Deployed watcher (wD:p1,
schema-1 state, claude jobs): no flag removals, ZERO store schema change, claude-path
behavior byte-identical. Do not deploy between commits; deploy only after commit 6 +
live drills.

## Appendix A — Codex limit surface inventory (evidence, 2026-07-31, this host)

Versions: codex-cli 0.146.0 (native binary under ~/.local/lib/node_modules/@openai/codex/
…/bin/codex), herdr 0.7.5. Evidence sources, cross-confirmed: (1) upstream
codex-rs/protocol/src/error.rs — banner strings byte-identical in installed binary via
`strings`; (2) ~/dev/unsnooze (user's prior auto-resume project: src/agents/codex.js,
src/watchers/codex.js, test/codex.test.js — fixtures captured from a REAL limit on 0.144);
(3) live captures of w6:p1/w7:p1 + 165MB rollout JSONL.

**A.1 Limit banner:** TUI does NOT exit. One red transcript line `■ {message}`, composer
stays live. Verbatim bodies:
- `You've hit your usage limit.` + tail variants
- `You've hit your usage limit for {limit_name}. Switch to another model now, or try again at {t}.`
- `You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at {t}.`
- `You've hit your usage limit. Upgrade to Plus to continue using Codex (https://chatgpt.com/explore/plus), or try again at {t}.`
- `You've hit your usage limit. To get more access now, send a request to your admin or try again at {t}.`
- `You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at {t}.`
- No-reset variants: `Your workspace is out of credits. Add credits to continue.` /
  `You hit your spend cap set in your workspace. Increase your spend cap to continue.` (+ owner variants)

**A.2 Reset tails:** same-day ` Try again at 3:51 PM.` (chrono %-I:%M %p, PROCESS-LOCAL
time, no tz printed, uppercase AM/PM, trailing period); cross-day (weekly)
` Try again at Feb 23rd, 2026 9:01 PM.` (English ordinal); ` Try again later.`;
older 0.144 form `Try again in 4 days 20 hours 9 minutes.` (keep for compat).

**A.3 Renders inline in transcript** (`■ ` red line). No footer banner. **No menu.**

**A.4 Chrome (0.146, live):** idle composer `› ` (U+203A ghost text, distinct from
Claude `>`/`❯`); footer `gpt-5.6-sol xhigh · ~/dev/x · x · main · … · Context 90% left ·
weekly 93% left` (middle-dot separated, may truncate); turn rule `─ Worked for 15m 04s ──…`;
working `• Working (12s • esc to interrupt)`; `• ` cells with `└` children;
`… +9 lines (ctrl + t to view transcript)`.

**A.5 Must-NOT-trigger:** heads-up warning `Heads up, you have less than {N}% of your
{5h|weekly|monthly} limit left. Run /status for a breakdown.`; overload `⚠ stream error:
exceeded retry limit, last status: 429 …; retrying 4/5 in 1.471s`; auth/upgrade prompts.

**A.6 Hooks:** none fire on usage limit (confirmed in unsnooze). Structured signal exists
only in rollout jsonl `token_count … rate_limits.{primary,secondary}.{used_percent,
window_minutes,resets_at}` — BACKLOG, not this phase. No positive rollout sample on this
host (never hit 100%); positive fixtures synthesized from verbatim strings + unsnooze's
live 0.144 captures, version-stamped.

**A.7 (BRIEF §21 Q1):** banner does not name the window; weekly manifests as the
cross-day date tail. Families keyed on body (usage-limit/model/credits/spend-cap).

**A.8 (BRIEF §21 Q2):** composer live after limit → typing + Enter resumes in-session.
**Esc must NOT be sent** (empty-composer Esc primes backtrack; second Esc edits previous
message). Codex ResumeAction = {KeysBefore: nil, Text: prompt, SubmitKey: enter}. Default
prompt = BRIEF §9.5 long prompt. `codex resume` CLI targets a new process — out of scope.

## Design decisions

- **D-P5-1:** new `internal/provider` (interface+registry), `internal/provider/claude`
  (thin delegation to detection — NOTHING moves out of internal/detection),
  `internal/provider/codex` (new analyzer; reuses internal/terminal +
  detection.ParseReset/ResetSpec/Analysis). Deps: jobs → provider → detection → terminal;
  coordinator → provider. Codex fills Analysis with MenuVisible=false always.
- **D-P5-2 interface:**
  ```go
  type ResumeAction struct { KeysBefore []string; Text string; SubmitKey string }
  type Provider interface {
      Name() string
      DetectContent(content string) bool                        // gate 6
      Analyze(content string, now time.Time) detection.Analysis
      SafeToResume(content string, now time.Time) (bool, string) // gate 9 + reason
      ResumeAction() ResumeAction
      AllowPeriodicNudge() bool // claude true; codex false
  }
  ```
  (BRIEF §7.1's DetectProcess/BuildResumeAction(ctx) simplified: identity checks are
  provider-neutral in validate.go; action is static per provider.)
- **D-P5-3 registry, AGENT-HINT WINS:** Pane.Agent set → that provider only, NO content
  fallback (herdr classification authoritative). Hint empty → content detection over
  enabled providers, bind only on exactly ONE match; two matches → none (log once).
  Safety: w7:p1 (codex pane developing unsnooze) prints Claude banners daily — content-
  wins would send Esc+continue into Codex's backtrack. Failure mode must be "no action".
- **D-P5-4:** codex.Analyze self-contained: own quote-mask (fences, `> `), codex chrome
  trim (A.4), bottom-most banner, stale guard, busy/overload guard (`esc to interrupt`,
  `retrying \d+/\d+`, `stream error` in live tail → Actionable=false). Banner is one line
  → no line-pairing engine. Reuses terminal.Lines/Tail + detection.ParseReset only.
- **D-P5-5 reset normalization → ParseReset:** extract tail after `(?i)(?:or )?try again`;
  `at`-form → strip `at `, trailing `.`, ordinals (`23rd`→`23`) → ParseReset (local-clock /
  date-time paths); `in`-form → pass whole multi-unit duration line; `later`/credits/
  spend-cap → IsLimited=true, Actionable=false (park + notify; NO periodic nudge).
- **D-P5-6 jobs:** Job.Provider stamped from LimitEvent.Provider (cfg.Provider only as
  fallback for deployed compat). Gate 6 → prov.DetectContent; gate 9 → prov.SafeToResume;
  beginResume → coordinator.SendResumeAction(rt, paneID, action, sleep);
  SendContinueSequence stays exported as the claude wrapper (byte-identical esc→text→enter,
  same 100ms sleep). Unknown provider in state → MANUAL_REQUIRED, never act. NO schema
  change.
- **D-P5-7 config = CLI flags only:** `--providers claude,codex` (default both),
  `--claude-prompt`, `--codex-prompt`. YAML config deferred to Phase 6/7. Prompts are
  text-only fields — no shell risk.
- **D-P5-8 codex SafeToResume:** IsCodex && (banner visible || idle composer `› `) &&
  no busy/overload marker.
- **D-P5-9 coordinator:** WithProviders(registry); nil → default claude-only registry
  (TUI/tmux + every existing test untouched). Poll resolves per pane via Pane.Agent,
  sets PaneState.Provider (new field; HasClaudeCode keeps name/meaning for TUI), periodic
  path gated on AllowPeriodicNudge, LimitEvent gains Provider.
- **D-P5-10 fixtures:** `codex0.146_*` (strings/live-chrome derived), `codex0.144_*`
  (unsnooze captures). Same sanitization policy as Phase 4; keep `■ › · ─` glyphs exact.

## Commits

1. **Provider abstraction + claude parity (no behavior change).** provider.go,
   registry.go, provider/claude/claude.go (delegates IsClaudeCode/Analyze; gate-9 body
   lifted VERBATIM from validate.go; action esc/"continue"/enter; prompt override);
   coordinator.SendResumeAction (+SendContinueSequence delegates); arch test walks
   ../provider, forbids provider→{jobs,store,runtime/*,tui,os/exec}. Tests: registry
   resolution table (hint match, unknown hint→none, no-hint single, no-hint both→none,
   disabled→none); claude characterization vs direct detection calls (incl. menu +
   starship fixtures); SendResumeAction key-order equality vs old sequence on Fake.
2. **Codex corpus + analyzer core.** provider/codex/codex.go (IsCodex, families:
   usage-limit/model-usage-limit/credits/spend-cap, banner anchors per A.1),
   reset.go (D-P5-5), testdata/positive/ per A.1×A.2 embedded in real chrome:
   at-sameday, at-crossday-ordinal, model-switch, pro-upgrade, plus-upgrade,
   admin-request, later-noreset, credits, spendcap, 0.144 relative-multiunit.
   Corpus walk asserts IsLimited/Family/Kind/ParsedTime fake-clock (cross-day pinned
   across year boundary; times pinned in non-host zone). IsCodex positives (sanitized
   live chrome) + negatives (claude chrome, shell, psql tables). Package-local — not
   wired until commit 4.
3. **Codex live-tail guards + negative corpus (BEFORE any wiring).** live.go (chrome
   trim, quote mask, stale guard, busy guard, Analyze); testdata/negative/:
   headsup_warning, stream_error_retrying, working_esc_to_interrupt,
   stale_scrollback_newer_output, claude_pane_content, quoted_in_prose (w7:p1 hazard),
   code_fence_quote, auth_upgrade_plus, composer_user_typed_banner. Cross-provider BOTH
   directions: add codex_pane_content.txt to claude negative corpus, assert claude
   Analyze → Actionable=false. Prose-probe negatives for every chrome classifier.
4. **Coordinator provider-aware polling.** WithProviders; per-pane resolve (Pane.Agent);
   prov.Analyze replaces direct detection calls in Poll/checkPaneRateLimit; periodic
   gated on AllowPeriodicNudge; sendContinue→sendResume(prov.ResumeAction()); test-
   pattern uses resolved provider action (claude fallback); PaneState.Provider;
   LimitEvent.Provider. Tests: codex pane full LimitEvent, zero periodic on unknown-
   reset codex fixture, hint-wins (claude banner in Agent=codex pane → zero events),
   no-hint ambiguity, existing claude coordinator tests green unmodified.
5. **Jobs provider-aware.** Registry option; HandleLimit stamps event.Provider (fallback
   cfg.Provider); gates 6/9 via provider; beginResume via SendResumeAction; unknown
   provider → MANUAL_REQUIRED; logs gain provider=. Tests: full codex job cycle (exactly
   one SendText(codex prompt)+enter, ZERO KeyEscape); codex pane replaced by shell →
   MANUAL_REQUIRED; claude byte-identity (esc→"continue"→enter); pre-Phase-5 schema-1
   state fixture loads+validates; provider "gemini" in state → MANUAL_REQUIRED.
6. **CLI wiring + detect --provider + docs.** runcmd flags (--providers,
   --claude-prompt, --codex-prompt; registry built once for coordinator+jobs);
   detectcmd --provider claude|codex; status table gains PROVIDER column; README;
   PROGRESS.md; BACKLOG.md (+rollout resets_at epoch as future signal; credits-park UX).
   Tests: flag parsing, detect goldens (codex positive + heads-up negative), E2E-style
   fake-runtime codex full cycle through runCommand components.
7. **(Orchestrator)** live E2E drills + deploy to wD:p1 + PROGRESS record.

## Live E2E drills (orchestrator)

**Gate status: OPEN — live Claude acceptance failed on 2026-07-31.** The deployed
watcher was alive and targeted `wA:p1` with `--interval 10m --verify-timeout 30m`,
but no state file/job was created for the visible `resets 10am (UTC)` episode. At
inspection time, newer output and an idle prompt followed the banner, so `detect`
returned `IsLimited=false` by the intentional stale-output guard. A manual fail-stop
`continue` submission succeeded and Herdr reported the pane `working`, isolating the
observed failure to limit acquisition/scheduling rather than pane input.

The most likely missed-transition mechanism is supported by the current code path:
startup polls while modes are off, `EnableAll` does not invoke the job sink, and the
next action-capable poll waits for `--interval`. The exact transition was not
instrumented. The deployed binary also predates the Phase 5 `detect --provider` CLI,
but current HEAD retains the same startup ordering, so the design gap still applies.

Before this live gate can close:

1. Claude validates `review.md` finding 8 against current HEAD and chooses the narrow
   acquisition design without weakening stale-output safety.
2. Add a failing fake-clock regression where a limit is present during startup and
   disappears before the first configured ticker event.
3. Implement the selected immediate post-enable and event/short-cadence acquisition
   behavior, then pass the normal build/vet/race gate.
4. Rebuild and install current HEAD, restart `wD:p1`, and repeat the long-interval live
   drill. Evidence must show one durable job, one resume submission, and terminal
   `RESUMED` state.

1. Codex simulated-banner full cycle: scratch /bin/cat pane staged with a REAL-format
   banner (`■ You've hit your usage limit. Upgrade to Pro … or try again at H:MM AM.`
   local time uppercase, trailing period) + codex chrome (composer `› ` + footer) so
   content detection binds (cat panes carry no agent label). Expect provider=codex,
   kind=local-clock, restart mid-WAITING, exactly ONE SendText(long prompt)+enter,
   NO escape, RESUMED attempts=1.
2. Dry-run cross-day form (`…or try again at Feb 23rd, 2027 9:01 PM.`) → kind=date-time.
3. Negative hint-wins on REAL codex pane w7:p1 (dry-run, 3 min): zero jobs — echoed/
   quoted banners fail guards; Claude-looking banners must NOT make claude jobs there.
4. Negative ambiguity: cat pane with claude banner + codex chrome → both detectors
   match → no provider → zero jobs (2 min dry-run).
5. Deployed compat: status/inspect against the real state file (PROVIDER column, claude
   jobs intact) → then upgrade wD:p1.

## Risks

1. Banner anchors land (c2) before guards (c3): package-local until c4; never deploy
   mid-branch.
2. `›` vs `>`: codex composer must never satisfy claude IsIdlePrompt — cross-provider
   negatives pin both directions.
3. w7:p1 prints limit fixtures daily — hint-wins is load-bearing; drill 3 pins it.
4. Codex prints process-local time, no tz: accept (same exposure as Claude local-clock);
   document.
5. Ordinal/`at` normalization corrupts schedules if wrong: table tests incl. 12:00 AM
   cross-midnight + Dec→Jan rollover.
6. Periodic-nudge leak would type into a live composer every 15 min: AllowPeriodicNudge
   =false + zero-send test.
7. TUI keeps nil-registry default; constructors stay back-compatible.
8. Tests constructing jobs.Config{Provider:"claude"} stay green via fallback stamp.

## Phase 5.5 — Review remediation (appended after review.md triage, 2026-07-31)

All ten review.md findings validated against HEAD by the orchestrator (spot-checked 1/2/4/7
in code; 8 confirmed by the live miss; 3/9/10 by the review's probes). Remediation on THIS
branch before merge, in review.md's suggested order, telescoped into four commits:

R1. **Transactional state + authoritative cancel (finding 1).** flock-based file lock
    around every state-file transaction (CLI cancel + manager saves); before EVERY
    transition AND immediately before sending input, re-read the job from disk and require
    expected prior state; mismatch → treat as concurrent modification, fail closed
    (adopt external terminal states). Promote the review's cancellation probe into a
    permanent regression test (two store handles).
R2. **Fail-stop input + honest non-persistent path (findings 2, 6).** SendResumeAction
    returns on first failed op (table tests: failure at each step asserts no later
    calls); sendResume propagates errors; ContinueSent/periodic state and notifications
    only on full success; failures logged/notified distinctly, no retry loop (one-shot
    stays).
R3. **Send-time identity + verification semantics (findings 3, 4).** At validation:
    re-resolve provider from the pane's CURRENT Agent hint + content; require match with
    job.Provider; conflicting non-empty hint → MANUAL_REQUIRED. Verification success
    requires the limit evidence to be non-live (provider Analyze: !IsLimited OR
    (IsLimited && !Actionable i.e. banner stale under newer output)) — never bare
    hash-change; episode fingerprint from normalized banner evidence line (not whole
    screen) to stop duplicate re-arm on unrelated output changes (adjust HandleLimit
    dedupe accordingly).
R4. **Acquisition blind window + config coherence + smalls (findings 5, 8, 7, 9, 10).**
    (a) Immediate action-capable poll right after EnableAll at startup. (b) Decouple
    detection from scheduling: detection/acquisition ticks every min(interval, 30s)
    while status logging and job advancement keep --interval cadence; verification runs
    on the short ticker too (fixes 5 structurally); ALSO reject --verify-timeout <
    2×interval at flag parse (fail fast). (c) doctor decodes the real protocol from
    status/snapshot JSON; unknown → WARN not PASS. (d) --margin 0 honored (default only
    when flag unset). (e) reject impossible calendar dates (Feb 31 → unknown; leap-year
    tests). Regression: banner visible at startup, gone before first long tick →
    job still created by the immediate/short-cadence poll (fake-clock).
Gate per commit unchanged. review.md finding 8's full event-driven acquisition remains
Phase 6 (socket events); R4's short ticker is the interim fix.
