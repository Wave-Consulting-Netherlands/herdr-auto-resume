# BACKLOG

Ordered follow-ups with rationale. Not scheduled; pull into PLANS.md when picked up.

1. **`clear`/`ack` command for parked jobs.** A pane with any non-RESUMED terminal job
   (MANUAL_REQUIRED, FAILED, CANCELLED, SESSION_GONE) is parked — `HandleLimit` never
   creates a new job for it until the state file is hand-edited. Safe, but there is no
   CLI verb to acknowledge/clear a handled job. Found during Phase 3 live E2E.
2. **`status` RESET column shows UTC despite the `RESET(local)` header.** Cosmetic;
   render in the local timezone or rename the header.
3. **Closed in Phase 4. Validation gate 9 `❯` sensitivity.** `Analyze` now treats a
   bare `❯` as an idle prompt and reserves MANUAL_REQUIRED for a detected menu block.
4. **goreleaser/CI still build under the upstream `autoclaude` name** — revisit at
   packaging (BRIEF Phase 7).
5. **PATH for Go toolchain** (`~/.local/go/bin`) still per-shell export on this host;
   belongs in the chezmoi-managed dotfiles.
6. **Codex rollout `resets_at` epoch integration.** Codex rollout JSONL carries structured
   `rate_limits.*.resets_at` epochs; integrate this as a future signal when rollout
   transcript support is scheduled, without changing the terminal fallback.
7. **Codex credits-park UX.** Workspace credits and spend-cap banners are detected and
   notified as non-actionable parked limits; add an explicit credits/park resolution
   command when the job acknowledgement workflow is designed.
8. **Done — Claude review and triage of `review.md`.** All ten findings were validated,
   ordered in PLANS.md Phase 5.5, and remediated with permanent regression coverage.
9. **Phase 6 code complete — pending live acceptance.** The event-driven socket client,
   reconnect/resync path, and short-cadence polling fallback now address the transient
   Claude acquisition miss. The orchestrator must still run the repeated BACKLOG-9 live
   drill, detach/reattach drill, pane-move drill, and soak before closing this item.

10. **Default transport flip after soak.** Keep `--transport cli` as the default until
    the explicit socket-mode soak and live drills are clean; then make the default flip
    a small, separately reviewed change.
