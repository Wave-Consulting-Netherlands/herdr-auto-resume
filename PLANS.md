# PLANS — Phase 8: Socket default flip, tolerant startup, parked-job ack (v0.3.0 + v0.4.0)

_Locked via grill — by Claude + Wave Consulting, 2026-08-01._

Supersedes the completed Phase 7 plan. Closes BACKLOG 1, 6, 7, 10, 11. Exit criteria:
**socket transport is the default on evidence, not hope; a watcher survives a Herdr restart
that outlives it; no terminal job can park a pane with no way out.**

Gate per commit: `go build ./... && go vet ./... && go test ./... -race -count=1`
(Go 1.26.5 at ~/.local/go/bin, GOCACHE=/tmp/herdr-go-cache,
GOMODCACHE=/tmp/herdr-go-mod-cache). Store schema stays 1 throughout (D-P8-5).

Deployed watchers during this phase:

- `herdr-auto-resume.service` — production, cli transport, `monitoring.panes: [wA:p1]`,
  `state.json`.
- `herdr-auto-resume-soak.service` — socket transport, `--pane wR:p1 --pane wS:p1`,
  `soak-state.json`, on released v0.2.0; the evidence clock restarts at step 6.

## Sequencing

**S0 ops (now, no release) → S1 soak evidence (48h) → S2 v0.3.0 flip → S3 v0.4.0 features.**
The flip ships in a release of its own precisely so that a post-flip regression has one
obvious cause. Features wait.

## Decisions

- **D-P8-1 Soak validity = uptime AND a forced cycle.** Idle uptime proves the socket stays
  connected; it does not prove detect → WAITING → resume → RESUMED still works over the event
  path. Clean requires all of: (a) ≥48h from the drill-pane provisioning restart (step 6; the
  2026-08-01 06:56 UTC start is superseded) with no unexplained restarts of the soak unit;
  (b) zero socket/reconnect/subscription errors in
  `journalctl --user -u herdr-auto-resume-soak.service`; (c) ≥1 forced full cycle passing
  under socket transport on the same v0.2.0 binary.
  Criterion (b) is evaluated over the window **ending when the T+48h drill begins**, and is
  frozen and recorded in PROGRESS.md at that moment. The deliberate reconnect drill (step 9)
  will itself produce reconnect entries; those fall in an explicitly timestamped
  expected-error window recorded beside the frozen gate and are judged on whether the watcher
  recovered — not on their absence. Without that split the gate contradicts its own
  reinforcement step.
- **D-P8-2 The forced cycle runs THROUGH the soak watcher, on a pre-provisioned harness pane.**
  Two corrections from review round 1: (a) `--test-pattern` is not a lifecycle drill — it calls
  `sendResume` directly (`coordinator.go:213`) and never creates a durable job, so it proves
  only that key injection works over the socket; (b) a fresh third watcher opens a fresh
  subscription, which cannot prove the 48-hour-old stream still delivers events, and stream
  longevity is the entire point of the soak. Therefore: provision a long-lived drill pane NOW,
  add it to the soak watcher's pane list, and restart the soak once at provisioning time
  (cheap — the clock has barely started). The drill fires at T+48h through the aged connection.
  The harness is a script, not `/bin/cat`: it prints a real provider limit banner with a
  near-future reset time, blocks on one line of input, then clears the banner and prints a
  distinct marker — so detection sees valid evidence, exactly one resume lands, and
  verification observes cleared evidence rather than a banner stuck in scrollback.
- **D-P8-3 No binary change touches the soak watcher once the clock starts.** Any code landing
  during S1 invalidates S1. The D-P8-2 restart happens before the clock starts and is the last
  restart. Tolerant-startup work (D-P8-4) is written and merged but NOT deployed until S2.
- **D-P8-4 Tolerant startup behind `--wait-for-panes`, default off — covering list ERRORS, not
  just empty results.** Review round 1 is correct that the flag as first specified would not
  have fixed the outage that motivated it: startup returns 1 on `ListPanes` error
  (`runcmd.go:474`) before the empty-match check is ever reached, and the observed 166 restarts
  were `list panes: ConnectionRefused` during a Herdr server bounce — not empty matches. Under
  the flag, zero matches and **retryable** list errors enter a rate-limited retry loop (fixed
  backoff, logged once per state change, not per attempt) that is context/signal-aware so
  SIGTERM still stops the process promptly. Retryable means transient reachability only —
  connection refused, socket absent, timeout, EOF mid-request. Permanent failures still exit 1
  even under the flag: protocol-version mismatch, malformed or undecodable responses,
  permission/authentication denials, and configuration errors such as an unsupported transport
  or an unusable socket path. Caveat from review round 3: a mistyped socket path and a socket
  that simply has not been created yet both surface as `ENOENT` and are indistinguishable at
  runtime, so a valid-but-absent path stays RETRYABLE; only a syntactically invalid path (empty,
  non-absolute after expansion, or over the ~108-byte `sun_path` limit) fails fast.
  A service that retries a protocol mismatch every 5s forever is a worse
  outcome than the crash loop this flag exists to remove — it looks healthy and monitors
  nothing. Default stays fail-fast so an interactive run still tells you the
  pane ID is wrong; both shipped units set the flag. Startup-only behavior — it does not touch
  the running event path, which is why it can share the v0.3.0 release with the flip.
- **D-P8-5 `ack <job-id-prefix>` as a RELEASED TOMBSTONE, expressed in metadata, no schema
  bump (BACKLOG 1 + 7).** Two corrections from review round 1. (a) A new terminal state alone
  unparks nothing: `handleLimitLocked` (`manager.go:198`) suppresses a new job unless the
  existing job is terminal **and** `State == StateResumed` **and** the evidence hash differs,
  so `ACKED` would block exactly like `FAILED` does today. The dedup condition must treat an
  acknowledged job as released: identical evidence stays suppressed (no resume loop on an
  unchanged banner), changed evidence may create a new job. (b) A schema bump is the wrong
  tool: `JSONStore.Load` accepts any version, and a v0.2.0 binary reading an unknown `ACKED`
  state would treat it as non-terminal and keep the pane parked — a silent wrong-direction
  downgrade. So acknowledgement is recorded as `acked_at` (+ `acked_reason`) metadata on the
  existing terminal job; old readers see a familiar terminal state and behave as they do now.
  A defensive upper-version rejection guard ships anyway in v0.3.0 for future changes.
  Otherwise unchanged: explicit job-id prefix only, resolution and ambiguity handling mirror
  `cancel`, no `--all`, no TTL auto-expiry, credits parks get a clear `inspect` reason string
  rather than a second verb.
  **Transition matrix (exhaustive, nothing implied):** terminal-and-not-RESUMED → ACKED, the
  only success path. Active states (WAITING, and any in-flight resume/verify state) → rejected,
  "job is still active; cancel it first" — ack must never race the scheduler for a job it may
  be mid-resume on. RESUMED → rejected as a no-op, nothing is parked. Already-acked → rejected
  as a no-op, exit 0 so a repeated ack in a script is not an error. Unknown or ambiguous prefix
  → rejected exactly as `cancel` does.
  **Transaction contract:** prefix resolution, eligibility check, mutation, and save happen
  inside ONE store transaction against a freshly loaded snapshot under the existing `.lock` —
  never resolve from a snapshot read before the lock and write it back after. A concurrent
  watcher+CLI test must prove a job that changed state between read and lock is re-evaluated,
  not clobbered.
- **D-P8-6 Codex `resets_at` corroboration is GATED ON A SPIKE, and fails closed (BACKLOG 6).**
  Review round 1 is right that the correlation is unspecified and unsafe as written: providers
  receive content only, pane metadata carries no Codex session identifier (live `pane list`
  shows `agent_session` for claude panes and none for the codex pane), and several Codex panes
  can share a cwd — so "newest rollout JSONL" could schedule one pane from another pane's
  reset. Step 18 is therefore preceded by a spike that must demonstrate a **verified unique
  pane→rollout mapping**; if it cannot, BACKLOG 6 is deferred rather than approximated. Given
  a mapping: resolution is an injected pane-aware interface (testable, no global filesystem
  reach from the provider layer); ambiguity, concurrent candidates, missing, unreadable, or
  unparseable files all fail closed to the terminal-parsed value. The merge rule is explicit —
  named JSON field, an explicit tolerance window, and **never earlier than the terminal value**
  (a too-early resume is the harmful direction) — with divergence logged once per evidence
  hash at INFO, not per poll. "Silent fallback" from the round-0 wording is withdrawn: absence
  is DEBUG, disagreement is INFO.
  **Unresolved by design — these are the spike's output, not the implementer's judgment:** the
  exact JSON pointer, the file-selection rule, the tolerance duration, and which rate-limit
  window is primary when several are present. Step 16 must land a PLANS.md amendment recording
  all four plus fixture evidence before step 18 may begin; an implementer who reaches step 18
  and finds this paragraph unamended must stop rather than choose values.
- **D-P8-7 Damped recycle ships in v0.4.0, after the flip (BACKLOG 11).** Poisoned sub-30s
  windows degrade to the 30s ticker floor — a latency defect, not a correctness one. Changing
  recycle timing in the flip release would invalidate the soak evidence the flip is gated on.
  Fix it on a known-good socket baseline: recycle immediately after each trigger-poll with
  ~5s identical-content damping, extending `recycleDue`.
- **D-P8-8 The flip's blast radius is `--runtime tmux` FIRST, `--session` second — resolved by
  a written precedence matrix.** Review round 1 caught the larger break: `runcmd.go:141`
  rejects socket transport unless runtime is herdr, so a default flip turns every
  `--runtime tmux` invocation into a hard startup error, and the same applies to the tmux TUI
  path. `--session` is the second case. Resolution: transport defaults to socket **only when
  the effective runtime is herdr and no `--session` is in play**; otherwise it defaults to cli
  with a warning naming the flag that caused the fallback. Explicitly requested impossible
  combinations (`--transport socket --runtime tmux`, `--transport socket --session x`) keep
  erroring exactly as today — the fallback applies to defaults, never to an explicit request.
  "Explicit" means `flag.FlagSet.Visit` saw the flag, or the YAML set the key; the matrix
  covers built-in default / YAML / flag × herdr / tmux × session-set / unset, for **both**
  `run` and `doctor` (which resolves configuration on its own path, `doctorcmd.go:70`), and
  every row gets a test.

## Work

### S0 — ops hardening (no release; partly done 2026-08-01)

1. Done: soak restarted as `~/.config/systemd/user/herdr-auto-resume-soak.service`; production
   `monitoring.panes` trimmed `[w1:p1, w4:p1, wA:p1, w6:p1, w7:p1]` → `[wA:p1]`; both services
   verified (`panes=1` / `panes=2` via throwaway `--state-file off --dry-run` instances,
   `doctor` PASS on both state files). Recorded in PROGRESS.md, commit 62a4a80.
2. Add `.codex-review/` to `.gitignore` (grill scratch, not an artifact).
3. README Troubleshooting: pane IDs are per-session and go stale when workspaces close; a
   watcher that restarts every 5s with "none of the requested panes were found" means a stale
   `monitoring.panes`, not a broken install.

### S1 — soak evidence (48h clock restarts at drill-pane provisioning)

4. Provision the drill harness (D-P8-2): a long-lived scratch workspace whose pane runs a
   script that idles until signalled, then prints a real provider limit banner with a reset
   ~2 minutes out, blocks on one line of stdin, and on receiving it clears the banner and
   prints a distinct marker. Validate the banner text against the committed detection fixtures
   with `herdr-auto-resume detect --provider … --file …` BEFORE it is used as evidence — a
   drill that fails because the banner was never recognized proves nothing about transport.
   Detection preflight is necessary but not sufficient: terminal clearing, line discipline
   (does the harness's blocking read actually consume the injected line?), and what
   verification observes after the clear are all still unproven at this point.
5. **Rehearse the whole cycle before the clock starts.** Run one complete throwaway
   WAITING → RESUMED cycle against the harness using a disposable watcher — same v0.2.0
   binary, same socket transport, same `--lines` read depth, same provider action, same
   terminal setup, throwaway state file. This rehearsal is explicitly NOT evidence (fresh
   connection, D-P8-2); its only job is to fail now rather than at T+48h. Fix the harness and
   repeat until it passes. Only then:
6. Restart the soak unit once with the drill pane appended to its pane list. **This restart
   starts the 48h clock**; no further restarts (D-P8-3).
7. Passive: daily `journalctl --user -u herdr-auto-resume-soak.service` sweep for restarts and
   socket errors; `doctor --transport socket --state-file …/soak-state.json` PASS.
8. At T+48h, freeze and record criterion (b) in PROGRESS.md, then signal the harness and assert
   through the aged connection: detection → WAITING job persisted in `soak-state.json` →
   exactly one resume → RESUMED with attempts=1 → verification saw cleared evidence. Record the
   transcript and job id.
9. Reinforcement on the same aged watcher, after the drill and after the gate is frozen:
   reconnect across a deliberate `herdr server stop`/start, and a pane move across tabs
   (terminal_id follow). Journal entries from this window are expected and timestamped as such.

### S2 — v0.3.0: the flip (only after S1 is clean)

10. Transport-default resolution moves behind one shared helper used by `run` AND `doctor`,
    implementing the D-P8-8 matrix; `--transport cli` remains fully supported. Tests: every row
    of defaults/YAML/flag × herdr/tmux × session-set/unset, in both commands, plus
    explicitly-requested impossible combinations still erroring and explicit-cli parity.
11. `--wait-for-panes` + `monitoring.wait_for_panes` (D-P8-4), TDD. Tests: off ⇒ list error
    exits 1 and zero matches exits 1 (today's behavior, pinned); on ⇒ retryable list errors
    (connection refused, absent socket, timeout, mid-request EOF) retry and recover when the
    runtime returns, **permanent errors (protocol mismatch, malformed response, permission
    denial, configuration error) still exit 1**, zero matches retries and picks the pane up
    when it appears, backoff is rate-limited, the loop logs once per state change, and a
    cancelled context/SIGTERM exits promptly rather than spinning; config/flag precedence
    including set-to-default.
12. Store upper-version rejection guard (D-P8-5b): a state file whose version exceeds the
    known schema is refused with a clear error instead of being silently coerced.
13. Docs: README (socket is now the default, when it silently falls back to cli and why, cli
    opt-out), both packaging units switch to `--transport socket --wait-for-panes`,
    config.example.yaml, BACKLOG 10 closed.
14. Release v0.3.0 via `scripts/release.sh`; verify an asset checksum and `version` output;
    install; restart both services on the new binary; `doctor` both transports.
15. 12h post-flip confirmation on production before S3 starts. A regression here reverts to
    `--transport cli` in the unit — one word, no rollback release.

### S3 — v0.4.0: features

16. `ack` verb (D-P8-5), TDD: `acked_at`/`acked_reason` metadata round-trip; every row of the
    transition matrix including active-state and RESUMED rejection and the idempotent
    already-acked exit 0; the one-locked-transaction contract, proved by a concurrent
    watcher+CLI test where the job changes state between read and lock; the dedup change in
    `handleLimitLocked` — acked + identical evidence stays suppressed, acked + changed evidence
    creates a new job, non-acked terminal states unchanged; an end-to-end watcher-reload test
    proving the pane is genuinely unparked after a restart; prefix resolution, ambiguous
    prefix, unknown id; `status`/`inspect` rendering; a v0.2.0-shaped state file still loads.
17. Codex `resets_at` spike (D-P8-6 gate): confirm a unique pane→rollout mapping against
    codex-cli 0.146 with two concurrent Codex panes sharing a cwd. Output is a PLANS.md
    amendment fixing the JSON pointer, file-selection rule, tolerance duration, and
    primary-window selection, with fixture evidence. **If no unique mapping exists, stop here
    and defer BACKLOG 6** with the finding recorded.
18. Given a mapping AND the step-17 amendment: corroboration behind an injected pane-aware
    resolver, TDD against committed fixtures — agreeing epoch refines, disagreement beyond
    tolerance keeps the terminal value and logs once per evidence hash, never-earlier rule
    holds, ambiguous or concurrent candidates fail closed, missing/corrupt file falls back at
    DEBUG.
19. Damped recycle (D-P8-7), TDD on `recycleDue` timing, plus a live poisoned-window drill
    under 30s to prove the gap actually closed.
20. Release v0.4.0; close BACKLOG 1, 7, 11 — and 6 only if step 17 cleared its gate.

## Risks / open questions

1. **The soak may never see a real limit event.** D-P8-1 accepts synthetic evidence; a genuine
   organic cycle under socket is nice-to-have, not a gate. If the flip later proves wrong, the
   forced-cycle drill is the artifact to re-examine first.
2. **Two watchers, disjoint panes, one Herdr.** Production covers `wA:p1`, soak covers
   `wR:p1`/`wS:p1`. If either pane list is ever edited to overlap, both watchers can resume the
   same pane — state dedup is per-state-file and offers no cross-watcher protection.
3. **`wA:p1` is this Claude session's own pane.** The production watcher monitors the pane an
   agent session runs in; self-pane exclusion only applies to the watcher's own `HERDR_PANE_ID`,
   which systemd units do not inherit.
4. **Ack correctness lives in the dedup rule, not the verb** (D-P8-5). The CLI surface is
   trivial; the risk is `handleLimitLocked` — too loose and an unchanged banner triggers a
   resume loop, too tight and the pane is still parked. The end-to-end reload test is the
   real acceptance criterion, not the unit tests around the verb.
5. **BACKLOG 6 may be undeliverable** (D-P8-6). Herdr exposes `agent_session` for claude panes
   but not for the codex pane observed live; if no unique pane→rollout mapping exists, the
   honest outcome is deferral, not a heuristic that can schedule one pane from another pane's
   reset.
6. **Flipping the default is a behavior change for existing installs** that never set
   `--transport`. Semver minor per §18, called out in release notes; `doctor` output makes the
   active transport obvious.
7. **The 48h clock restarts** if the soak unit is restarted for any reason, including a Herdr
   server bounce that outlives its retry window.

## Out of scope

Herdr-native TUI and plugin packaging (D-P7-3/4 deferrals stand); pane selection by stable
identity such as workspace label or cwd (noted as a successor to D-P8-4, not this phase);
BRIEF §11 logging/notification config sections; any change to the tmux runtime or TUI;
upstream-tag hygiene beyond leaving v0.1.x alone.
