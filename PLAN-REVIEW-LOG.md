# Plan Review Log: Phase 8 — socket default flip, tolerant startup, parked-job ack

Act 1 (grill) complete — plan locked with the user in PLANS.md. MAX_ROUNDS=5.
Reviewer: Codex (codex-cli 0.146.0, gpt-5.6-sol, reasoning effort xhigh), read-only every round.

## Round 1 — Codex

Material problems found:

1. **The forced-cycle drill cannot prove the claimed lifecycle.** `--test-pattern` can send directly without creating a durable job, while `WAITING → RESUMED` requires a recognized provider banner and successful verification; `/bin/cat` also leaves the limit banner in scrollback. See [PLANS.md](/home/ubuntu/dev/Herdr-auto-resume/PLANS.md:36) and [coordinator.go](/home/ubuntu/dev/Herdr-auto-resume/internal/coordinator/coordinator.go:198).  
Fix: Use a deterministic pane harness that emits a valid near-term limit banner, accepts one resume action, then replaces the banner with verifiably safe output.

2. **A fresh third watcher does not validate the 48-hour-old event connection.** Long uptime plus a forced cycle through a new connection cannot prove the soak watcher’s stream still delivers events.  
Fix: Run the forced cycle through the original long-lived soak watcher using a preconfigured disposable pane, restarting the soak clock if necessary.

3. **`--wait-for-panes` does not cover the failure it is meant to fix.** The observed outage is `ListPanes: ConnectionRefused`, but the plan only tolerates a successful empty result; current startup exits before the empty-pane check on any list error. See [runcmd.go](/home/ubuntu/dev/Herdr-auto-resume/runcmd.go:474).  
Fix: Under the flag, retry both list errors and empty matches in a signal-aware, rate-limited loop, with tests for connection refusal, recovery, and cancellation.

4. **Adding `ACKED` will not unpark anything under current deduplication.** Every terminal state other than `RESUMED` permanently suppresses new jobs for that pane, so `ACKED` would remain a blocker. See [manager.go](/home/ubuntu/dev/Herdr-auto-resume/internal/jobs/manager.go:181).  
Fix: Define ACKED as a released tombstone: identical evidence stays suppressed, changed evidence may create a new job, and add an end-to-end watcher-reload test.

5. **The proposed schema bump does not make old binaries fail loudly.** `JSONStore.Load` accepts every version, and an old reader treats unknown `ACKED` as non-terminal, creating unsafe downgrade behavior. See [json_store.go](/home/ubuntu/dev/Herdr-auto-resume/internal/store/json_store.go:57) and [store.go](/home/ubuntu/dev/Herdr-auto-resume/internal/store/store.go:23).  
Fix: Prefer an old-known terminal state plus backward-compatible acknowledgment metadata; if schema 2 is unavoidable, ship an upper-version rejection guard in v0.3 and prohibit older downgrades.

6. **The default flip misses the `tmux` blast radius and leaves “explicit” undefined.** Defaulting transport to socket makes `--runtime tmux` fail, while YAML socket plus `--session` has no specified fallback/error behavior; doctor also resolves configuration differently. See [runcmd.go](/home/ubuntu/dev/Herdr-auto-resume/runcmd.go:127) and [doctorcmd.go](/home/ubuntu/dev/Herdr-auto-resume/doctorcmd.go:70).  
Fix: Add a source-precedence matrix covering defaults, YAML, and flags for Herdr/tmux/session in both `run` and `doctor`, then test every row.

7. **Pane-to-rollout correlation is unspecified and can cross-contaminate sessions.** Providers receive only content, pane metadata lacks a Codex session identifier, and multiple Codex panes may share a CWD; choosing the newest JSONL could schedule one pane from another pane’s reset. See [provider.go](/home/ubuntu/dev/Herdr-auto-resume/internal/provider/provider.go:16) and [runtime.go](/home/ubuntu/dev/Herdr-auto-resume/internal/runtime/runtime.go:4).  
Fix: Specify an injected pane-aware resolver with a verified unique session mapping; ambiguity or concurrent candidates must fail closed and be tested.

8. **The corroboration algorithm is not implementable deterministically as written.** No tolerance, primary/secondary rate-limit selection, conservative merge rule, or consistent logging policy is defined; the plan simultaneously says INFO logging and silent fallback.  
Fix: Define exact JSON field selection, tolerance boundaries, a never-earlier merge rule, and once-per-evidence logging levels with boundary fixtures.

VERDICT: REVISE
### Claude's response

All eight findings accepted; three were verified against the code first and were my errors, not style disagreements:

- **F1 (drill is not a lifecycle test)** — confirmed at `coordinator.go:213`: `--test-pattern` calls `sendResume` directly and never reaches the job manager. D-P8-2 rewritten around a scripted banner harness validated against the committed detection fixtures.
- **F4 (ACKED unparks nothing)** — confirmed at `manager.go:198`: suppression lifts only when the prior job is terminal AND `StateResumed` AND evidence differs. The dedup rule is now the deliverable; the verb is incidental.
- **F6 (tmux blast radius)** — confirmed at `runcmd.go:141`: `socket transport requires --runtime herdr`. The flip would have broken every tmux invocation, not just `--session`. D-P8-8 is now a full precedence matrix over both `run` and `doctor`.
- **F3** — confirmed at `runcmd.go:474`: startup returns 1 on list error before the empty-match check, so the flag as written missed the very outage that motivated it. Now covers list errors with a signal-aware, rate-limited retry.
- **F5** — accepted in its stronger form: no schema bump at all. Acknowledgement becomes `acked_at` metadata on an existing terminal state so a v0.2.0 reader stays safe; the upper-version guard ships anyway as forward insurance.
- **F7/F8** — accepted; D-P8-6 is now gated on a spike proving a unique pane→rollout mapping, fails closed on ambiguity, and specifies tolerance, a never-earlier merge rule, and once-per-evidence-hash logging. Deferral is an acceptable outcome.
- **F2** — accepted in substance, rejected in remedy. Codex proposed running the drill through the soak watcher and restarting the clock "if necessary"; instead the drill pane is provisioned NOW and the single restart happens before the clock has value, so the T+48h drill runs through the aged connection without discarding evidence.

Nothing was rejected outright.

## Round 2 — Codex

Seven prior findings are substantively addressed; the corroboration algorithm is only partially resolved. Remaining material issues:

1. **Codex corroboration still contains placeholders.** “Named JSON field” and “explicit tolerance window” are promised but not specified, and the spike does not explicitly require updating the plan before implementation.  
Fix: Make the spike output a plan amendment recording the exact JSON pointer, file-selection rule, tolerance duration, rate-limit window selection, and fixture evidence before step 17 begins.

2. **`--wait-for-panes` retries every `ListPanes` error indefinitely.** That correctly handles connection refusal, but also masks permanent protocol, permission, authentication, malformed-response, and configuration failures in shipped services.  
Fix: Define typed retryable errors—connection refusal, missing socket, timeout—and fail fast on permanent protocol/configuration errors, with tests for both classes.

3. **The `ack` transition and transaction contract remains incomplete.** Step 15 does not explicitly reject active states and `RESUMED`, nor require prefix resolution, eligibility validation, and save against one fresh state snapshot under the lock.  
Fix: Add an explicit transition matrix and a concurrent watcher/CLI test proving the entire ack mutation is one locked fresh-load transaction.

4. **The harness is only preflighted for banner detection.** If terminal clearing, line discipline, or verification behavior is wrong, the problem will be discovered only after the 48-hour soak.  
Fix: Before starting the clock, run the harness through a complete throwaway `WAITING → RESUMED` cycle using the same binary, read depth, provider action, and terminal setup.

5. **The soak gate contradicts its reconnect drill.** D-P8-1 requires zero reconnect errors, while mandatory step 8 deliberately causes a reconnect and will likely produce those same journal entries.  
Fix: Freeze and record the clean gate before step 8, or define an explicitly timestamped expected-error exclusion for the deliberate reconnect interval.

VERDICT: REVISE
### Claude's response

Seven of eight round-1 findings confirmed addressed. All five new findings accepted; nothing rejected.

- **R2-1 (corroboration placeholders)** — the four unresolved values are now marked as the spike's OUTPUT, not the implementer's judgment, with an explicit stop instruction if step 18 is reached unamended.
- **R2-2 (indefinite retry masks permanent failures)** — the sharpest new finding: a service that retries a protocol mismatch every 5s forever looks healthy and monitors nothing, which is worse than the crash loop the flag exists to remove. Retryable is now an enumerated set (connection refused, absent socket, timeout, mid-request EOF); protocol mismatch, malformed responses, permission denials, and config errors still exit 1 even under the flag.
- **R2-3 (ack transaction contract)** — added an exhaustive transition matrix (active states and RESUMED rejected; already-acked idempotent at exit 0) and a one-locked-fresh-load-transaction requirement with a concurrent watcher+CLI test.
- **R2-4 (harness preflighted only for detection)** — added a full rehearsal cycle on a disposable watcher BEFORE the clock starts, explicitly labelled not-evidence; its only job is to fail now rather than at T+48h.
- **R2-5 (gate contradicts its own reconnect drill)** — a real self-contradiction in D-P8-1. Criterion (b) is now frozen and recorded at the moment the drill begins, with the deliberate reconnect's journal entries falling in a timestamped expected-error window judged on recovery, not absence.

Also swept stale cross-references introduced by the renumbering (steps 6/9/18, schema line, soak clock line).

## Round 3 — Codex

All five round-2 findings are addressed: the spike now amends the plan, startup errors are classified, `ack` has an atomic transition contract, the harness gets a full rehearsal, and the reconnect window is separated from the clean soak gate.

No new material blockers found. Minor implementation note: a mistyped nonexistent socket and a not-yet-created socket both produce `ENOENT`; only syntactically invalid paths can reliably fail fast, while valid missing paths should remain retryable.

VERDICT: APPROVED
### Claude's response

Converged at round 3 of 5. The minor note is folded into D-P8-4: a mistyped socket path and a not-yet-created one are indistinguishable (`ENOENT`), so a valid-but-absent path stays retryable and only syntactically invalid paths (empty, non-absolute after expansion, over the ~108-byte `sun_path` limit) fail fast.

**Outcome: 13 findings across 3 rounds, all accepted, one remedy substituted.** The plan changed shape in five places that would have cost real rework: the drill proved nothing, ACKED unparked nothing, the flip broke tmux, indefinite retry was worse than the crash loop, and the soak gate contradicted its own reinforcement step.
