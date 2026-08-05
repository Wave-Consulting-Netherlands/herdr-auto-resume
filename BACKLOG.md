# BACKLOG

Ordered follow-ups with rationale. Not scheduled; pull into PLANS.md when picked up.

1. **CLOSED in v0.5.0 (2026-08-05, commit 2fd52e0).** `ack <id-prefix> [--reason text]`
   releases a handled park inside one locked transaction; an acked job with CHANGED evidence
   creates a new job while identical evidence stays suppressed; `status` gained a PARKED column
   and `inspect` gained `parked`/`park_reason`, so the exclusion is no longer invisible — that
   visibility was half the defect. Original report below.
   **clear/ack command for parked jobs — PROMOTED to next-up (2026-08-05).** A pane with any
   non-RESUMED terminal job is parked; add a safe CLI verb to acknowledge or clear a handled
   job without hand-editing state. No longer hypothetical: `manager.go:279` returns "already
   owned" for any same-pane job that is terminal-but-not-RESUMED, so a stale park **silently
   excludes that pane from all future detection**. Found live on 2026-08-05 — four healthy,
   actively-working panes (w1F, w1B, w0, wZ) still carried MANUAL_REQUIRED jobs from earlier
   limits, which would have made the v0.3.1 menu fix inapplicable to the very two panes it was
   written for. Cleared by hand-editing state.json with the watcher stopped, because that is
   the only remedy that exists today. The verb must also make the exclusion visible (`status`
   should say a pane is parked and why) — a silent permanent exclusion is the worst property
   of the current behaviour.
2. **Closed in Phase 7 (D-P7-6).** Status RESET(local) now renders in the caller-provided
   local timezone; the Europe/Amsterdam regression prevents a UTC display regression.
3. **Closed in Phase 4.** Validation gate 9 sensitivity is covered by the committed regression
   tests and safe menu handling.
4. **Closed in Phase 7 (BACKLOG 4).** GoReleaser, CI, release workflow, script, and binary
   names now use herdr-auto-resume; the first release is v0.2.0.
5. **Out of repository.** The Go toolchain PATH belongs in the chezmoi-managed dotfiles, not
   this application repository.
6. **Codex rollout resets_at epoch integration — SPIKE PARTLY RUN 2026-08-05 (D-P8-6 / PLANS
   step 18); still blocked on one fixture.** Codex rollout JSONL carries structured
   `rate_limits.resets_at` epochs; integrate as a future signal without changing the terminal
   fallback. Evidence gathered from seven real rollouts written today:
   - **cwd is NOT a usable selector.** Three rollouts share `~/dev/Herdr-auto-resume`, two
     share `~/dev/workspace`, two share `~/dev/psft_instanceid`. Any cwd-based file-selection
     rule is ambiguous in ordinary daily use, not just in a contrived case.
   - **`session_id` is not 1:1 with the file either.** The two 20:41:00 rollouts, 145 ms
     apart, carry DIFFERENT top-level `id`s but the SAME `session_id`. File selection must
     therefore tolerate several files per session (latest-wins or merge) instead of assuming
     one.
   - **`originator` is the field that identifies pane-backed sessions:** `codex-tui` is a real
     pane; `codex_exec` and `Claude Code` are headless. Only `codex-tui` rollouts can
     correspond to a herdr pane, so the resolver must filter on it before matching.
   - **Still blocked:** every observed window is healthy (`used_percent` 14–16,
     `window_minutes` 10080). No exhausted-window fixture exists yet, so the primary/secondary
     selection rule and the tolerance duration remain unproven. Note the structural risk: our
     Codex usage is almost entirely headless, so a pane-backed exhausted rollout may never
     occur naturally here — if it does not, PLANS step 18's stop-and-defer condition applies.
7. **Codex credits-park UX.** Workspace credits and spend-cap banners are detected and
   non-actionable; add an explicit credits/park resolution command when acknowledgement is
   designed.
8. **Done — Claude review and triage of review.md.** All ten findings were validated, ordered,
   and remediated with permanent regression coverage.
9. **Closed in Phase 6.** Socket acquisition, reconnect/resync, pane-move, negative, and soak
   acceptance drills passed; the event-driven acquisition miss is recorded as resolved.
10. **Closed 2026-08-03 — default transport flip after soak.** The 48h socket soak and
    aged-connection drill passed (job `cdc0f5e1`: detection over a 50h-old event stream, durable
    WAITING job, exactly one resume, “resume verified”, and RESUMED). Production is now on the
    socket default; `--transport cli` remains the explicit opt-out.
17. **AUDIT DELIVERED 2026-08-05 (read-only Codex); adopt-list below, item stays open for the
    two adoptions.** Verdicts: `pane.agent_detected` — adopted in v0.4.0. `agent.explain` —
    adopt as a manual diagnostic (zero cost in the runbook). `agent_status_changed` — keep as a
    wake-up hint only; `blocked` is not rate-limit-specific and carries no reset time, so
    trusting it to authorize injection REDUCES safety. `agent.wait` — trap: status cannot prove
    the banner cleared or that our resume caused the transition. `agent.prompt` — lateral:
    proves submission, not acceptance, and has no revision precondition. `agent.start` — cannot
    replace the revive launcher: it has **no cwd parameter**, the exact bug the launcher exists
    to fix. Schema confirms no event carries a reset time and none covers the session-file
    cases. **Correction to the original note below:** we already subscribe to
    `pane.agent_status_changed` (`events.go:193`) and feed it in as a detection tick; the claim
    that we "stopped at output_matched" was wrong.
    **Audit the herdr agent API against our hand-rolled equivalents (2026-08-02).** Protocol 17
    exposes agent-level surface we never enumerated. Concretely, each of these has a hand-built counterpart in this repo:
    - `pane.agent_status_changed` pushes `idle|working|blocked|done`. **Both menu-parked panes
      today reported `blocked`** — herdr will tell us a pane is stuck instead of us inferring
      it from screen text on a 30s ticker. Candidate primary trigger, scraping as fallback.
    - `pane.agent_detected` — the creation-time signal BACKLOG 15 needs; already reachable
      through our existing subscription machinery, so that item is smaller than estimated.
    - `agent.prompt` / `agent.send-keys` — supported prompt submission above raw keystrokes;
      we hand-roll Esc→text→Enter in the provider ResumeAction.
    - `agent.wait --until <status>` — native "wait for state" primitive; our verification
      polls and diffs pane text instead.
    - `agent.start` — sanctioned way to start an agent in an existing pane; Phase D revive
      instead shells out via a launcher script invented after two failed drills.
    - `agent.explain` — "explain agent detection state", i.e. a debugger for exactly the class
      of mystery that cost a day on 2026-08-01.
    Not a defect: everything works. But detection could plausibly be event-driven with
    scraping as the fallback rather than the reverse. Caveats to keep: no event carries a
    RESET TIME (parsing stays), and none cover the session-file cases (limit with no pane,
    closed workspace), so the file channel remains the authority. Do this as a post-v0.3.0
    refactor with drills per replacement, not a rewrite.
18. **FIXED in v0.3.1 (2026-08-05, commit b142329) — pending a live drill.** The limit-menu
    signature now satisfies the chrome check when `answer_limit_menu` is on and the provider
    already resolved to claude; a terminal-ID gate was added so a reused pane id still parks;
    every other gate is unchanged; resume-time parks now emit one diagnostic line. 553 tests
    (was 544). **The live gate is still open** — this class of defect has twice survived a
    green suite, so the fix is not proven until a real menu pane resumes through it. Original
    report below.
    **DEFECT (found 2026-08-04, live): the identity gate parks menu-visible panes before the
    menu-answering branch can run — `answer_limit_menu` is unreachable in its own use case.**
    Two real sessions (w1F:p1 `dynamic-transfer2comp`, w1B:p1 `psft_pcode_diff`) were detected
    correctly via the session-file channel, admitted, and scheduled with high confidence for
    11:11Z. At the resume moment both parked MANUAL_REQUIRED, attempts=0,
    `last_validation: "pane is not claude"` — with `answer_limit_menu: true` configured.
    Cause: `internal/jobs/validate.go:75` runs `current.DetectContent(content)` and parks at
    line 76; the `AnswerLimitMenu` branch is line 91, downstream. When the limit menu owns the
    whole viewport, a 200-line `--source detection` read returns ~41 lines containing **no
    Claude chrome at all** (verified live on w1F:p1), so `detection.IsClaudeCode` is false and
    the gate fires first. The feature built for menu panes can never see a menu pane whose
    menu covers the screen.
    Fix direction: test `looksLikeLimitMenu(content)` before the generic content-identity gate
    (the menu text is itself a Claude-specific marker), or let a job whose `agent`/`terminal_id`
    and stored provider still match satisfy identity without re-detecting chrome. Keep every
    other gate — process, cwd, single-shot — unchanged; identity must still be established,
    just not exclusively from chrome the menu hides. Needs a drill against a real menu pane,
    plus a regression fixture of a menu-only viewport.
    Secondary: the park is **silent** — no journal line named the pane or the reason, so this
    was only visible by dumping state.json. The D-P8-10 diagnostics cover detection-time
    non-actions; extend the same one-line-per-reason treatment to resume-time parks.
15. **CLOSED in v0.4.0 (2026-08-05).** Shipped as `monitoring.admit_agent_events` (default
    off): `pane.agent_detected` subscription plus startup/resync snapshot seeding (D-S3a-1..7).
    Live-gated twice — the first gate passed while covering only 2 of 6 panes, which is how the
    seeding gap was found. Production went from `panes=1` to all seven live panes. Original
    report below.
    **Event-hook pane pickup (from mo-arvan/herdr-claude-auto-retry, reviewed 2026-08-02).**
    That plugin registers herdr `[[events]]` hooks on agent-detected and picks up new agent
    panes at creation, so coverage never depends on config or on a limit having fired. Our
    Phase B admission only triggers on a limit observation, so a brand-new pane is uncovered
    until it hits one — and static `monitoring.panes` needed three manual edits in two days.
    Adopt the same signal: subscribe to herdr agent-detected events (socket transport already
    has the subscription machinery) and admit matching agent panes under the existing
    consistency and self-pane gates. Keep it opt-in and per-episode-logged like D-P8-14; the
    review's authorization-boundary objection still applies — an agent label alone is not a
    licence to inject, so admitted-by-event panes must still pass every resume gate.
16. **Transient API failure backoff (same source).** A category we do not handle at all:
    429s, 5xx, "overloaded", "temporarily limiting requests", "api error: connection". They
    classify these separately from usage limits and retry with exponential backoff
    (60s base, 300s cap, doubling). Ours ignores them entirely — a transient stall is not a
    reset-time limit, so no job is created and the pane simply sits. Add a transient class to
    detection with its own bounded retry policy, explicitly distinct from reset-bearing
    limits, defaulting off until drilled. Note their retry is unverified fire-and-forget;
    ours must keep the verification and single-flight guarantees.
13. **CLOSED in v0.3.0 (SD-D4 phases A–D).** The session-file channel + `agent_session`
    correlation shipped and produced the first real-world unattended resumes on 2026-08-02
    (wW/wS/wX/wY RESUMED, attempts=1). Original report below.
    **Real limits produced no job (2026-08-01; DIAGNOSED 2026-08-02).** JSONL evidence corrected the initial report: failure #1 was session ce7bb791
    in pane wW:p1 (psft_run_script) — monitored by NO watcher, a coverage-model gap, not a
    code defect. Failure #2 was session 829d1239 in wA:p1 — watcher healthy, banner parses
    Actionable=true, so it was swallowed post-detection (menu-visible / not-auto / silent
    read error / read-window); the deployed diag build names the reason on the next
    occurrence. Fix direction: session-file detection channel + pane correlation via herdr
    `agent_session` (D4, needs sign-off).
14. **CLOSED — detection-time in v0.3.0 (D-P8-10), resume-time in v0.3.1.** Both classes of
    non-action now log one line per evidence hash / per park. The v0.3.1 half was added only
    after the silence hid BACKLOG 18 for a full day, which is the argument for treating any
    new silent exit path as a defect on sight.
    **Every non-action on a limited pane is silent (PLANS.md D-P8-10).** Menu visible, reset
    unparsed, provider unresolved, pane not enabled, horizon exceeded — all exit without a
    trace, which is what made item 13 undiagnosable. Needs one log line per evidence hash
    naming pane and reason.
12. **CLOSED in v0.3.0 (D-P8-9), shipped alongside `--wait-for-panes` as required.**
    **Pane enablement is decided once, at startup (found 2026-08-01, Phase 8 step-5 rehearsal).** `runcmd.go` runs `Poll() → EnableAll() → Poll()`
    once, and `EnableAll` skips panes whose provider has not resolved yet, leaving
    `Mode = ModeOff` with no later re-evaluation. A watcher that starts before its agent panes
    attach reports `panes=N` indefinitely and can never act on them. Reproduced live in both
    directions on one pane, banner, binary, and transport. Must land with `--wait-for-panes`,
    which otherwise converts a visible crash loop into a silent no-op.
20. **FIXED and drill-verified 2026-08-05 (same night it was found).** The rescue is local to
    the validate path: unresolved content-provider + `answer_limit_menu` on + EMPTY agent hint
    + stored provider claude + limit-menu content. Shared identity detection, both
    `SafeToResume` implementations and registry resolution are untouched. Re-drill evidence:
    job 02ca6136 on w1J:p1 reached the MENU BRANCH — refusal changed from
    `unknown-current-provider-for-pane` to `menu answer refused: missing session or episode
    identity`, which is the session guard, not an identity gate.
    **Bound discovered while proving it:** no synthetic pane can complete the menu answer.
    `answerLimitMenu` requires both a pane `agent_session` and a job episode, and a shell-script
    pane has neither. The guard is correct and must NOT be relaxed to make a drill pass — for
    real panes the episode registry synthesises an episode from `AgentSessionID`
    (`manager.go:191`), so there is no production gap. The menu keypress itself can therefore
    only be proven by a real Claude pane hitting a real limit menu. That is the one open live
    gate for BACKLOG 18.
    Original report below.
    **The v0.3.1 menu fix is incomplete: a menu-only pane with NO agent hint still parks
    (found 2026-08-05 by the first real menu drill).** v0.3.1 taught the CONTENT-identity gate
    (`validate.go` ~:91) that the limit menu counts as Claude. But `providers.Resolve(
    candidate.Agent, content)` runs earlier at `validate.go:68` and returns nil when the pane
    carries no agent hint AND the content identifies no provider — which is exactly a bare
    menu. The job then parks at :78 with `unknown current provider for pane`, before the menu
    branch is ever reached. Live drill evidence: job 53c22c26 on pane w1H:p1, scheduled
    `confidence=high` for 22:18:00Z, parked `reason=unknown-current-provider-for-pane`.
    **Scope, stated honestly:** the two REAL panes on 2026-08-04 (w1F/w1B) were tagged
    `agent=claude` by herdr, so Resolve succeeded for them and they parked at the later gate —
    the one v0.3.1 fixed. Production's common path is genuinely fixed. This is the narrower
    case where herdr has not tagged the pane (agent released or never detected). It is also
    why the drill harness cannot reproduce the production failure exactly: a shell script pane
    gets no agent hint, so the drill tests a STRICTER path than production hits.
    Fix: when the content-provider is unresolved but `answer_limit_menu` is on, the content
    looks like the limit menu, and the job's stored provider is claude, resolve to the stored
    provider — subject to the unchanged terminal-ID, process, cwd, and single-shot gates. Then
    re-run `scripts/menu-drill-harness.sh`; the pass condition is MENU-ANSWERED-OK plus a
    recorded MenuAttempt, NOT a RESUMED job.
19. **Minor, pre-existing: the JSON store chmods its PARENT DIRECTORY to 0700 on every save**
    (`internal/store/json_store.go:96`), so any state file whose directory is not owned by the
    user fails to save with `chmod <dir>: operation not permitted`. Hit while testing `ack`
    against a state file in /tmp. Harmless for the normal XDG state directory and NOT a
    regression — confirmed present on master before the ack work — but the failure message
    names chmod rather than the real problem, and tightening a directory the tool does not own
    is overreach. Fix: chmod only the state FILE, or ignore a chmod failure on a
    pre-existing directory.
11. **CLOSED 2026-08-05.** `recycleDue` now recycles immediately after a trigger-poll instead
    of on a one-minute floor, with a 5s identical-content damping window
    (`recycleDampingWindow`) and the trigger consumed each poll so a repeated trigger cannot
    spin. The polling fallback is unchanged — the recycle stays an optimisation, never the sole
    detection path. Original report below.
    **Poisoned-window sub-30s transients.** pane.output_matched does not refire within one
    subscription, so a stale detection window can consume the armed shot. Live drilling caught
    clean-window 2s transients and poisoned 40s banners; poisoned sub-30s windows remain a
    documented improvement opportunity. Candidate fix: recycle immediately after each
    trigger-poll with identical-content damping of about five seconds.
