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
  `--pane wV:p1` (drill harness), `soak-state.json`, on released v0.2.0.
  **Evidence clock started 2026-08-01 13:48:27 UTC; T+48h = 2026-08-03 13:48 UTC.**

## Sequencing

**S0 ops (done) → S1 soak evidence (48h, running) → SD real-world diagnosis (NEW, blocking) →
S2 v0.3.0 flip → S3 v0.4.0 features.**

SD was inserted on 2026-08-01 after two real Claude sessions hit plain usage-limit banners in
monitored panes (`wA:p1` under the production watcher, `wS:p1` under the soak watcher) and
produced **no job, no state file, and no log line**. Neither state file has ever existed, so
the tool has not been observed completing a real-world cycle in production — only in drills.

Transport defaults and startup tolerance are refinements to a tool whose core promise is
currently failing. They do not ship first. S1 and SD run concurrently (SD's evidence gathering
is read-only and does not disturb the soak), but **S2 does not begin until SD closes.**

## Decisions

- **D-P8-1 Soak validity = uptime AND a forced cycle.** Idle uptime proves the socket stays
  connected; it does not prove detect → WAITING → resume → RESUMED still works over the event
  path. Clean requires all of: (a) ≥48h from 2026-08-01 13:48:27 UTC (the step-6
  restart; the 06:56 UTC start is superseded) with no unexplained restarts of the soak unit;
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
  Two further constraints, both learned by building it (`scripts/soak-drill-harness.sh`):
  the IDLE screen must already identify as Claude, or D-P8-9 leaves the pane inert for the
  whole soak; and because an idle Claude pane receives a periodic nudge every 15 minutes, the
  harness must drain buffered input after printing the banner, or the blocking read consumes a
  months-old nudge and reports a resume that never happened. EOF is likewise not a resume.
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
  reset. Step 19 is therefore preceded by a spike that must demonstrate a **verified unique
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
  all four plus fixture evidence before step 19 may begin; an implementer who reaches step 19
  and finds this paragraph unamended must stop rather than choose values.
- **D-P8-9 Pane enablement must be re-evaluated, not decided once at startup (found by the
  step-5 rehearsal, 2026-08-01).** `runcmd.go:524-526` runs `Poll() → EnableAll() → Poll()`
  exactly once, and `EnableAll` only enables panes where `stateProviderActive` — provider
  already resolved. A pane whose agent is not identifiable at that instant gets
  `PaneState{Mode: ModeOff}` and is never revisited: the watcher reports `panes=N` forever and
  can never act on it. Reproduced live and both directions confirmed on the same pane, banner,
  binary, and transport — watcher started while the pane was unidentifiable ⇒ no job ever;
  watcher started while the same banner was visible ⇒ job created and resumed within seconds.
  Blast radius is production, not just the drill: a systemd watcher that starts before its
  agent panes attach is silently inert until someone restarts it, and this is exactly the
  ordering a boot-time unit produces.
  **This makes D-P8-4 dangerous to ship alone** — a watcher that patiently waits for panes to
  appear will then never enable them, converting a loud crash loop into a silent no-op. The
  fix therefore ships in the same release: re-evaluate mode for panes that become
  provider-active later (enable on transition, preserving any explicit user disable), with a
  regression test that starts a watcher against an unidentifiable pane, makes it identifiable,
  and asserts a job is created.
- **D-P8-10 Silence on a limited pane is a defect in its own right.** Every path that sees a
  limited pane and does NOT create a job exits without a trace today: menu visible, reset
  unparsed, provider unresolved, pane not enabled, horizon exceeded. That silence is why the
  2026-08-01 incident is undiagnosable after the fact — there is nothing to read. Requirement:
  when a pane is detected as limited and no job results, log exactly one line per evidence
  hash naming the pane and the specific reason. Once per evidence hash, not per poll, so a
  limit sitting on screen for hours does not flood the journal. This is not optional polish;
  without it the next occurrence is equally undiagnosable.
- **D-P8-11 The flip is gated on a real-world resume, not on drill evidence.** The soak proves
  socket transport carries a synthetic cycle. It does not prove the tool resumes a genuine
  Claude limit, which is what actually failed twice. v0.3.0 does not ship until at least one
  real limit has been detected, jobbed, and resumed on a real agent pane — or, if the root
  cause proves to be environmental rather than a code defect, until that is established from
  captured evidence and recorded here.
- **D-P8-12 The diagnostic build deploys to PRODUCTION only; the soak watcher stays on
  v0.2.0.** These are separate units with separate state files, so restarting the production
  watcher on an instrumented binary does not touch the soak or its clock (D-P8-3 constrains
  the soak watcher specifically). This buys fast real-world instrumentation without spending
  the 48h of socket evidence already accumulated.
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

### SD — real-world diagnosis (blocking; runs concurrently with S1)

- **D1. Ground-truth capture — DONE 2026-08-01** (`scripts/limit-capture.sh`, commit 414b8cc,
  running detached). Snapshots `wA:p1`/`wR:p1`/`wS:p1` on a signal deliberately broader than
  the detector's own patterns — matching only what the detector accepts would reproduce the
  blind spot under investigation. Read-only; bounded by content hash and per-pane cooldown.
  Captures land in `~/.local/state/herdr-auto-resume/limit-captures/`.
- **D2. Diagnostic logging (D-P8-10), TDD.** One line per evidence hash naming pane and reason
  whenever a limited pane yields no job. Ship on the `phase-8-flip` branch, build, and deploy
  to the PRODUCTION unit only (D-P8-12). Tests: each non-action reason logs exactly once;
  repeated polls of identical evidence do not re-log; changed evidence logs again.
- **D3. Diagnose from evidence — RESOLVED 2026-08-02 from Claude session JSONLs**
  (`~/.claude/projects/**/*.jsonl` carry structured rate_limit entries with sessionId, cwd,
  banner, timestamp; found by studying saaranshM/unsnooze):
  - Failure #1 (15:10:48Z, session ce7bb791, pane wW:p1, psft_run_script): **never
    monitored** — the pane was in no watcher's list. Coverage-model gap: strict opt-in panes
    plus ephemeral workspaces leave every new workspace unprotected by default.
  - Failure #2 (15:14:56Z, session 829d1239, pane wA:p1): **swallowed after detection** —
    watcher healthy all window, banner text parses Actionable=true/high, so the drop is
    menu-visible, not-auto, silent read error, or read-window; v0.2.0 cannot say which, the
    deployed diag build names each. Closed as far as pre-diag evidence allows.
  - Bonus: `herdr pane list` `agent_session` maps panes to session UUIDs for both providers —
    the D-P8-6 spike's "unique pane→rollout mapping" exists as a first-class API lookup.
- **D4 — approved 2026-08-02: session-file channel + menu answering + dead-session resume.**
  All four unsnooze advantages adapted to this architecture; the StopFailure hook is held in
  reserve as a latency upgrade only. Detailed plan below (SD-D4); decisions D-P8-13…D-P8-17.

### SD-D4 implementation plan

Verified data shapes this plan is built on (checked live 2026-08-02, not assumed):

- Claude session files `~/.claude/projects/<munged-cwd>/<sessionId>.jsonl`: rate-limit records
  carry `"error":"rate_limit"`, `apiErrorStatus:429`, `timestamp` (ISO event epoch),
  `sessionId`, `cwd`, `requestId` (natural dedup key), and the banner text (tz-tagged prose,
  e.g. "resets 4:30pm (UTC)") inside `message.content[].text`. **No reset epoch field** — the
  reset still goes through ParseReset, but with a deterministic timezone tag and a machine
  observation timestamp. 74 such records exist on this host; both 2026-08-01 failures are in
  them.
- Codex rollout files `~/.codex/sessions/YYYY/MM/DD/rollout-*-<sessionId>.jsonl`: carry
  `rate_limits` blocks with true `resets_at` epochs, BUT the shape varies by vintage
  (Feb files: primary window 300m + secondary 10080m; Aug files: primary 10080m,
  secondary null) and these records are routine telemetry — `resets_at` presence is NOT an
  exhaustion signal. The exhaustion predicate must come from a real exhausted record
  (D-P8-18); until one is captured the Codex channel is blocked. This supersedes D-P8-6's
  spike framing and Risk 5 (herdr DOES expose `agent_session` for codex panes — verified
  2026-08-02).
- `herdr pane list` / socket snapshot expose `agent_session.value` = the session UUID for both
  providers — pane↔session correlation is an API lookup.
- The rate-limit menu fixture shows the cursor defaulting to `❯ 1. Stop and wait for limit to
  reset` — answering it is a verified Enter, not navigation.

Decisions (revised after harden round 1 — 12 findings, all accepted; see PLAN-REVIEW-LOG.md):

- **D-P8-13 Session-file channel, as a new observation type feeding the EXISTING serialized
  loop.** New package `internal/sessionfile` produces a `SessionObservation`
  {provider, sessionID, cwd, observedAt, resetRaw, resetAt, requestID} — NOT a LimitEvent;
  LimitEvent requires pane+content+evidence a file record does not have. Observations are
  resolved against a fresh pane snapshot inside the coordinator's single event loop, becoming
  a job only for the exact live pane whose `agent_session` matches, with session/cwd/provider
  consistency checks. **Episode identity is provider+sessionID+resetAt(epoch)** and is shared
  with the scrape channel where computable: the herdr scrape path attaches the pane's
  `agent_session` to its events, so both channels dedupe on the same key. Where the key is
  NOT computable — tmux runtime, or a herdr pane with no `agent_session` yet — the scrape
  path keeps today's pane+evidence dedup unchanged and cross-channel dedup simply does not
  apply (legacy fallback, tested). Relative resets ("try again in 5 hours") produce
  different epochs per observer; episode equality therefore uses tolerance matching per
  D-P8-22. **Durability (192h jobs, v0.2.0 writers):** the episode dedup registry lives
  in the scan sidecar (which old binaries never touch), keyed
  provider+sessionID+resetBucket; the Job's episode field is a convenience mirror only — a
  v0.2.0 read-modify-write cycle dropping it must not break dedup, and a test proves that
  round trip. Job records `source: session-file|scrape`.
  **The channel itself is a gated feature:** `providers.session_file_channel: false` default
  (flag + YAML key). The channel, its sidecar, and `revive` all REQUIRE a persistent state
  path — with `--state-file off` they refuse to start with a clear error rather than running
  memory-only.
- **D-P8-14 Targeted admission, NOT blanket discovery.** `discover_agent_panes` as originally
  specified was an authorization expansion disguised as opt-in (an agent label is not an
  input-injection boundary). Replaced: a pane outside `monitoring.panes` may be admitted ONLY
  when a fresh session-file observation names its exact `agent_session`, behind
  `monitoring.admit_session_matches: true` (flag + YAML; default off; units may set it after
  the Phase-A live gate). Admission is per-episode, logged, obeys D-P8-9 and self-pane
  exclusion, and requires a complete pane snapshot with exactly one match — zero or multiple
  matches, or a partial snapshot, fail closed with a diagnostic. Prerequisite: both herdr
  adapters must parse and retain `agent_session` (they currently discard it).
- **D-P8-15 Menu answering — default OFF, single-shot, persisted, TOCTOU documented.** Herdr
  offers no revision-conditional send, so read-then-Enter is inherently racy; automatic
  answering therefore ships disabled (`resume.answer_limit_menu: false`). When enabled:
  require the literal question line AND `❯` on "Stop and wait for limit to reset" in a read
  taken immediately before the send; persist the attempt (session+episode) BEFORE sending so
  a daemon restart cannot re-answer; send exactly one Enter; re-read and log the outcome.
  Dry-run = log-only, no send, no cleared-menu expectation. Any other menu state stays
  fail-closed. If Herdr later ships a revision-conditional send, switch to it and drop the
  caveat.
- **D-P8-16 Dead-session resume — operator verb first, automation later.** Automatic spawn
  has an unfixable check-then-act race against other watchers, user resumes, and delayed
  metadata without a cross-process per-session lease. Phase D therefore ships
  `herdr-auto-resume revive <session-id-prefix>`: an explicit operator command that takes a
  per-session lease file, re-checks an unfiltered fresh pane snapshot for the session under
  the lease, refuses on any match, then spawns `claude --resume <id>` in a daemon-owned
  workspace and hands the pane to the normal verify path. Automation on top of the proven
  lease + ATTACHING-intent mechanics is a later, separately reviewed change. Claude only;
  Codex deferred.
- **D-P8-17 Version scope: v0.3.0 = the whole phase-8-flip branch (S2 + diagnostics + D4
  phases that pass their gates).** Supersedes "the flip ships alone". Each risky feature is
  additionally gated per D-P8-18; the flip itself stays soak-gated and one-word revertible.
- **D-P8-18 Per-feature live gates — nothing ships enabled on unit-test evidence alone.**
  Phase A (Claude live-pane channel): enabled after one live drill where a session-file
  observation creates a job for a monitored pane. `admit_session_matches`: enabled only after
  a live drill admitting an unmonitored pane correctly. Menu answering: only after a live
  menu drill on a real limit menu. Codex channel: blocked until a REAL exhausted rollout
  record is captured (shapes vary across versions — Feb files show primary=300/secondary=10080,
  reviewer-inspected Aug files show primary=10080/secondary=null; `resets_at` presence is
  routine telemetry, NOT an exhaustion signal — the predicate must come from a real record).
  `revive`: after a live dead-session drill. Features whose gate has not passed stay off in
  shipped units.
- **D-P8-21 Sidecar concurrency contract.** The sidecar is shared mutable state between the
  watcher and the `revive` process, so atomic rename alone is not enough. Contract: every
  sidecar mutation is a read-modify-write under an exclusive flock on `<state>.scan.lock`
  (same discipline as the store's transactional `.lock`); fixed lock ordering everywhere —
  sidecar lock first, then the store `.lock`, never the reverse; writes that span both files
  (pending observation → job) go PENDING(sidecar) → job(store) → COMMITTED(sidecar), and
  startup reconciles PENDING-without-job (retry) and job-without-COMMITTED (mark committed)
  so a crash between the two writes converges instead of double-creating. Tests: concurrent
  watcher+revive mutation, lost-update (two readers, both write), crash at each seam.
- **D-P8-22 Episode equality uses tolerance matching, not buckets.** A 5-minute floor has a
  boundary cliff: observations seconds apart can straddle it, and the same relative banner
  observed minutes apart derives different epochs regardless of bucket size. Replaced: two
  observations are the same episode iff same provider+sessionID and |resetAtA − resetAtB| ≤
  10 minutes; the registry stores the first-seen resetAt per episode and matches new
  observations by nearest-within-tolerance. Boundary tests replace bucket tests (4:59:59 vs
  5:00:01 apart; same relative banner observed 3 minutes apart on both channels).
- **D-P8-23 Session-identity features require a session-identity runtime.** Config
  validation rejects `session_file_channel`, `admit_session_matches`, and
  `answer_limit_menu` (its persisted-attempt key needs a session) when runtime is tmux, at
  startup, with an error naming the flag and the reason. Tmux keeps today's scrape-only
  behavior; no silent degradation.
- **D-P8-19 Scanner cursors live in a versioned SIDECAR, not the state file.**
  `manager.go` saves fresh `File{Version, Jobs}` literals — any additive cursor field would be
  silently erased on the next job save, and a v0.2.0 rollback would drop it without tripping
  the upper-version guard. Cursors go to `<state>.scan.json` (own version, atomic rename, same
  0600/backup discipline). Bootstrap: start at EOF with a bounded lookback (default 2h) so
  first startup cannot replay 74 historical records — with `revive` manual-only, replay cost
  is bounded even if misconfigured. Ordering: persist observation intent before any side
  effect; advance the cursor only after durable acceptance. Tests cover crash between intent
  and side effect, partial lines, truncation, and inode replacement (rotation).
- **D-P8-20 File discovery is strict.** Claude: only `~/.claude/projects/<project>/<uuid>.jsonl`
  top-level session files; records with `isSidechain: true` are rejected (subagent sidechains
  are not resumable sessions); malformed session IDs rejected. Codex: only
  `rollout-*-<uuid>.jsonl` under the dated tree.

Steps (TDD; gate per commit; phases are independently shippable):

- **D4.0** Both herdr adapters parse and retain `agent_session` (prerequisite for all
  correlation). Fixture-driven tests for CLI and socket transports.
- **D4.1 (Phase A)** `internal/sessionfile` Claude scanner: strict discovery (D-P8-20),
  sidecar cursors with EOF+lookback bootstrap (D-P8-19), requestId dedup,
  SessionObservation output. Fixtures are the real sanitized 2026-08-01 records; the
  acceptance test is that BOTH failure events yield observations with correct session, cwd,
  and reset (16:30Z).
- **D4.2 (Phase A)** Episode identity per revised D-P8-13: registry in the scan sidecar,
  convenience mirror on Job, tolerance-based episode matching (D-P8-22), legacy fallback where the key is
  not computable, sidecar mutations under the D-P8-21 lock contract. Tests: delayed-file-after-scrape, scrape-after-file, tmux no-session
  fallback, relative-reset observed at different times, and the v0.2.0 read-modify-write
  round trip (old writer drops the Job field; dedup must survive via the sidecar).
- **D4.3 (Phase A)** Coordinator wiring: observations resolved in the event loop against a
  fresh snapshot; monitored-pane match → job via existing sink; consistency checks.
  **Zero matches is NOT terminal:** the observation persists as pending in the sidecar and
  is retried against fresh snapshots until expiry (herdr populates `agent_session`
  asynchronously — a lagging label must not lose the event). Expiry and freshness rules:
  a pending observation expires at min(resetAt + verify_timeout, observedAt + 24h); no job
  is ever created for an episode whose resetAt is already more than `margin` in the past
  (stale bootstrap evidence from the 2h lookback must not resume a recovered session).
  Cursor advance = observation durably persisted (accepted or pending), never
  matched-or-dropped. Tests: lagging agent_session then match; expiry; stale-reset
  rejection; crash between persist and cursor advance. Live drill, then enable Phase A in
  production (D-P8-12 pattern).
- **D4.4 (Phase B)** `admit_session_matches` per D-P8-14 with its live gate.
- **D4.5 (Phase C)** Menu answering per D-P8-15 (default off), tests on the committed fixture
  plus adversarial variants (cursor on option 2, unrelated menu, missing question line,
  restart-no-reanswer), live gate before any unit enables it.
- **D4.6 (Phase D)** `revive` verb per D-P8-16, with the crash-safe attach protocol IN this
  step, not deferred: persist an ATTACHING intent (session, timestamp, lease) BEFORE spawning;
  transition to ATTACHED/job on success; on startup, reconcile any dangling ATTACHING record
  against a fresh unfiltered snapshot before honoring new revives. Tests: concurrent revive;
  pane appears between check and spawn under the lease; stale lease recovery; crash
  immediately BEFORE spawn (intent present, no pane) and immediately AFTER spawn (pane
  present, no job) — both must reconcile without a duplicate attach.
- **D4.7** Codex scanner — BLOCKED until a real exhausted rollout fixture exists; then as
  D4.1 for Codex with epoch resets.
- **D4.8** SD closes per D-P8-11 on the first real end-to-end resume through any gated-on
  channel; each phase's gate result recorded in PROGRESS.md.

### S2 — v0.3.0: the flip — DONE 2026-08-04 (steps 10–16 complete; see PROGRESS.md)

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
12. Pane re-enablement fix (D-P8-9), TDD — **required in the same release as step 11**, not
    optional: a pane that becomes provider-active after startup transitions to ModeAuto and
    produces jobs; an explicitly disabled pane stays disabled; the existing startup path is
    unchanged for panes identifiable at t=0.
13. Store upper-version rejection guard (D-P8-5b): a state file whose version exceeds the
    known schema is refused with a clear error instead of being silently coerced.
14. Docs: README (socket is now the default, when it silently falls back to cli and why, cli
    opt-out), both packaging units switch to `--transport socket --wait-for-panes`,
    config.example.yaml, BACKLOG 10 closed.
15. Release v0.3.0 via `scripts/release.sh`; verify an asset checksum and `version` output;
    install; restart both services on the new binary; `doctor` both transports.
16. 12h post-flip confirmation on production before S3 starts. A regression here reverts to
    `--transport cli` in the unit — one word, no rollback release.

### S3 — v0.4.0: features

17. `ack` verb (D-P8-5), TDD: `acked_at`/`acked_reason` metadata round-trip; every row of the
    transition matrix including active-state and RESUMED rejection and the idempotent
    already-acked exit 0; the one-locked-transaction contract, proved by a concurrent
    watcher+CLI test where the job changes state between read and lock; the dedup change in
    `handleLimitLocked` — acked + identical evidence stays suppressed, acked + changed evidence
    creates a new job, non-acked terminal states unchanged; an end-to-end watcher-reload test
    proving the pane is genuinely unparked after a restart; prefix resolution, ambiguous
    prefix, unknown id; `status`/`inspect` rendering; a v0.2.0-shaped state file still loads.
18. Codex `resets_at` spike (D-P8-6 gate): confirm a unique pane→rollout mapping against
    codex-cli 0.146 with two concurrent Codex panes sharing a cwd. Output is a PLANS.md
    amendment fixing the JSON pointer, file-selection rule, tolerance duration, and
    primary-window selection, with fixture evidence. **If no unique mapping exists, stop here
    and defer BACKLOG 6** with the finding recorded.
19. Given a mapping AND the step-17 amendment: corroboration behind an injected pane-aware
    resolver, TDD against committed fixtures — agreeing epoch refines, disagreement beyond
    tolerance keeps the terminal value and logs once per evidence hash, never-earlier rule
    holds, ambiguous or concurrent candidates fail closed, missing/corrupt file falls back at
    DEBUG.
20. Damped recycle (D-P8-7), TDD on `recycleDue` timing, plus a live poisoned-window drill
    under 30s to prove the gap actually closed.
21. Release v0.4.0; close BACKLOG 1, 7, 11 — and 6 only if step 18 cleared its gate.

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
