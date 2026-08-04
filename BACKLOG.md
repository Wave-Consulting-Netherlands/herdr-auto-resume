# BACKLOG

Ordered follow-ups with rationale. Not scheduled; pull into PLANS.md when picked up.

1. **clear/ack command for parked jobs.** A pane with any non-RESUMED terminal job is parked;
   add a safe CLI verb to acknowledge or clear a handled job without hand-editing state.
2. **Closed in Phase 7 (D-P7-6).** Status RESET(local) now renders in the caller-provided
   local timezone; the Europe/Amsterdam regression prevents a UTC display regression.
3. **Closed in Phase 4.** Validation gate 9 sensitivity is covered by the committed regression
   tests and safe menu handling.
4. **Closed in Phase 7 (BACKLOG 4).** GoReleaser, CI, release workflow, script, and binary
   names now use herdr-auto-resume; the first release is v0.2.0.
5. **Out of repository.** The Go toolchain PATH belongs in the chezmoi-managed dotfiles, not
   this application repository.
6. **Codex rollout resets_at epoch integration.** Codex rollout JSONL carries structured
   rate_limits.resets_at epochs; integrate this as a future signal without changing the
   terminal fallback.
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
17. **Audit the herdr agent API against our hand-rolled equivalents (2026-08-02).** Protocol 17
    exposes agent-level surface we never enumerated — we subscribed to `pane.output_matched`
    and stopped. Concretely, each of these has a hand-built counterpart in this repo:
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
18. **DEFECT (found 2026-08-04, live): the identity gate parks menu-visible panes before the
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
15. **Event-hook pane pickup (from mo-arvan/herdr-claude-auto-retry, reviewed 2026-08-02).**
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
13. **Real limits produced no job (2026-08-01; DIAGNOSED 2026-08-02, fix pending; PLANS.md
    SD-D3/D4).** JSONL evidence corrected the initial report: failure #1 was session ce7bb791
    in pane wW:p1 (psft_run_script) — monitored by NO watcher, a coverage-model gap, not a
    code defect. Failure #2 was session 829d1239 in wA:p1 — watcher healthy, banner parses
    Actionable=true, so it was swallowed post-detection (menu-visible / not-auto / silent
    read error / read-window); the deployed diag build names the reason on the next
    occurrence. Fix direction: session-file detection channel + pane correlation via herdr
    `agent_session` (D4, needs sign-off).
14. **Every non-action on a limited pane is silent (PLANS.md D-P8-10).** Menu visible, reset
    unparsed, provider unresolved, pane not enabled, horizon exceeded — all exit without a
    trace, which is what made item 13 undiagnosable. Needs one log line per evidence hash
    naming pane and reason.
12. **Pane enablement is decided once, at startup (found 2026-08-01, Phase 8 step-5 rehearsal;
    scheduled as PLANS.md D-P8-9 in v0.3.0).** `runcmd.go` runs `Poll() → EnableAll() → Poll()`
    once, and `EnableAll` skips panes whose provider has not resolved yet, leaving
    `Mode = ModeOff` with no later re-evaluation. A watcher that starts before its agent panes
    attach reports `panes=N` indefinitely and can never act on them. Reproduced live in both
    directions on one pane, banner, binary, and transport. Must land with `--wait-for-panes`,
    which otherwise converts a visible crash loop into a silent no-op.
11. **Poisoned-window sub-30s transients.** pane.output_matched does not refire within one
    subscription, so a stale detection window can consume the armed shot. Live drilling caught
    clean-window 2s transients and poisoned 40s banners; poisoned sub-30s windows remain a
    documented improvement opportunity. Candidate fix: recycle immediately after each
    trigger-poll with identical-content damping of about five seconds.
