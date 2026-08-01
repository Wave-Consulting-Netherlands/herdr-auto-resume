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
10. **Default transport flip after soak.** Keep --transport cli as the default until the
    explicit socket-mode soak and live drills are clean; then make the default flip a small,
    separately reviewed change.
13. **Real limits produced no job on two monitored panes (2026-08-01, OPEN — highest
    priority; PLANS.md milestone SD).** Two Claude sessions hit plain usage-limit banners in
    `wA:p1` (production watcher) and `wS:p1` (soak watcher). No job, no state file, no log
    line; neither state file has ever existed. The menu fail-closed path is ruled out (plain
    banner) and both panes were being monitored. Root cause unknown — the failing path leaves
    no artifact and the pane text is gone. `scripts/limit-capture.sh` now captures ground
    truth for the next occurrence.
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
