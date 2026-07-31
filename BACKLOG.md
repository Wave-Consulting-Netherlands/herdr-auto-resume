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
