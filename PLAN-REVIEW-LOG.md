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

# Harden: SD-D4 plan — 2026-08-02 06:42Z

Act 1 skipped per standing user rule ("harden" = Act 2 only). Artifact: PLANS.md SD-D4 section. MAX_ROUNDS=5.

## Round 1 — Codex

REVISE. The SD-D4 plan has several release-blocking gaps.

1. **Codex’s claimed shape is wrong.** The inspected Aug 1–2 rollouts place limits at `.payload.rate_limits`; they contain `primary.window_minutes=10080` and `secondary=null`, not primary=300/secondary=10080 as claimed in [PLANS.md](/home/ubuntu/dev/Herdr-auto-resume/PLANS.md:270). Worse, these are routine `token_count` records, not exhaustion evidence, so `resets_at` alone is not a limit signal.  
Fix: Block D4.2 until a real exhausted record establishes the exact JSON pointer, exhaustion predicate, selected window, and negative cases for ordinary telemetry/null windows.

2. **“Same LimitEvent” is not a valid abstraction.** [LimitEvent](/home/ubuntu/dev/Herdr-auto-resume/internal/coordinator/coordinator.go:35) requires a concrete pane, terminal content, and scrape evidence; a dead-session record has none of those but has session/request IDs the type cannot carry.  
Fix: Introduce a normalized session observation first, then resolve it into either a live-pane `LimitEvent` or a separately persisted dead-session action.

3. **The two channels do not share a dedup identity.** Existing dedup in [manager.go](/home/ubuntu/dev/Herdr-auto-resume/internal/jobs/manager.go:176) hashes scrape evidence by pane; request-ID dedup is scanner-local, so delayed/replayed file evidence can differ from scrape evidence and re-arm after RESUMED, while concurrent menu and resume input remain unserialized.  
Fix: Give both channels one durable episode key such as provider+sessionID+reset epoch and feed observations through one serialized coordinator event loop.

4. **Dynamic discovery is an authorization expansion disguised as opt-in.** Setting `discover_agent_panes` in both shipped units makes the practical default “monitor every Herdr-classified agent,” while current [runtime.Pane](/home/ubuntu/dev/Herdr-auto-resume/internal/runtime/runtime.go:4) and [paneInfo](/home/ubuntu/dev/Herdr-auto-resume/internal/runtime/herdr/parse.go:58) do not even retain `agent_session`; an agent label alone is not a safe input-injection boundary.  
Fix: Leave discovery unset in shipped units and admit only the exact pane whose `agent_session` matches a fresh validated session-file event, with session/CWD/provider consistency checks.

5. **Menu answering has an unavoidable TOCTOU in the proposed API.** The plan reads text and later sends unconditional Enter, but pane revision is discarded and [Runtime](/home/ubuntu/dev/Herdr-auto-resume/internal/runtime/runtime.go:16) offers no compare-and-send; a re-read afterward cannot undo Enter delivered to a changed prompt or shell.  
Fix: Require a server-side revision/session-conditional send under a per-pane action lock; if Herdr cannot provide that primitive, keep automatic menu answering disabled.

6. **Menu attempt semantics are internally contradictory and non-durable.** Dry-run cannot both skip Enter and require the menu to disappear, while “one attempt per evidence hash” will repeat after daemon restart unless persisted.  
Fix: Define dry-run as log-only/no-job and persist attempts by session+episode+pane revision before sending the key.

7. **Dead-session resume still has the exact double-attach race identified by the plan.** “List panes, observe none, create pane” is not atomic against another channel, watcher with a different state file, user resume, or delayed Herdr session metadata; the existing per-state-file run lock cannot prevent this.  
Fix: Acquire a cross-process per-session lease, persist an ATTACHING intent, perform a fresh unfiltered authoritative pane lookup under that lease, and spawn only on exactly zero matches.

8. **The cursor is not safely additive to schema v1.** [store.File](/home/ubuntu/dev/Herdr-auto-resume/internal/store/store.go:63) has only version/jobs, and job creation currently saves a fresh literal that would erase any cursor; old v0.2 readers also ignore and later drop additive fields, while the `Version > 1` guard cannot detect this semantic loss.  
Fix: Put scanner cursors in a separately versioned atomic sidecar, or explicitly preserve them through every store transaction and prohibit rollback to readers that drop them.

9. **Cursor delivery semantics are unspecified and potentially catastrophic.** First startup could replay all 74 historical top-level Claude failures and, with dead resume enabled, create many panes; advancing before durable handling loses events, while advancing afterward can repeat external spawn after a crash.  
Fix: Specify EOF/bounded-lookback bootstrap, persist observation intent before side effects, advance only after durable acceptance, and test crash points, partial lines, truncation, replacement, and inode changes.

10. **Session discovery needs cardinality and file-boundary rules.** There are also nested subagent rate-limit records; recursive discovery can treat non-resumable sidechains as top-level sessions, and the three-case routing omits duplicate `agent_session` matches and failed/partial pane snapshots.  
Fix: Scan only `<project>/<UUID>.jsonl`, reject sidechains and malformed IDs, and fail closed unless a complete pane snapshot yields exactly zero or one matching pane.

11. **D4.7 cannot validate what D4 ships.** “One real resume through any channel” may exercise only scrape or a live Claude pane while untested Codex parsing, menu input, workspace creation, cleanup, and dead-session attachment are enabled in shipped units.  
Fix: Keep each risky feature off until its own live gate passes, including a real Codex exhaustion fixture, revision-safe menu drill, and crash/restart dead-session attach drill.

12. **The smaller safe design is missing.** A session-file observation can target only its exact matching live pane and enter the existing serialized coordinator; absent sessions can notify and expose an explicit resume command instead of globally discovering panes and automatically spawning workspaces.  
Fix: Ship the Claude live-session channel first, defer Codex and automatic dead-session attachment, and make dead resume an explicit operator action until locking and lifecycle behavior are proven.

VERDICT: REVISE
### Claude's response

All 12 accepted; 4 were re-verified against code/data before accepting (F1 shape variance confirmed across rollout vintages; F4 confirmed — neither adapter retains agent_session; F5 confirmed — no revision-conditional send in the herdr API; F8 confirmed — manager.go:236 saves File literals that would erase a cursor field). The plan was restructured rather than patched:

- F12 (smaller safe design) became the spine: phased shipping A→D, Claude live-pane channel first, everything risky off by default behind per-feature live gates (new D-P8-18).
- F2 → SessionObservation type resolved in the serialized coordinator loop (D-P8-13). F3 → shared episode key provider+sessionID+resetAt on the job (D4.2).
- F4 → blanket discover_agent_panes REPLACED by per-episode targeted admission keyed to a fresh session-file observation (D-P8-14); units do not pre-enable it.
- F5/F6 → menu answering default OFF with documented TOCTOU, persisted attempt-before-send, dry-run=log-only (D-P8-15).
- F7 → automatic dead-session spawn replaced by an explicit operator verb `revive` with a per-session lease; automation deferred (D-P8-16).
- F8/F9 → cursors moved to a versioned sidecar with EOF+bounded-lookback bootstrap and intent-before-side-effect ordering (D-P8-19). F10 → strict discovery + sidechain rejection + exactly-one-pane rule (D-P8-20). F11 → per-feature live gates (D-P8-18). F1 → Codex channel explicitly BLOCKED on a real exhausted fixture (D4.7).

Nothing was rejected.
