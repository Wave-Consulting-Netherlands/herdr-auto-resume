# PLANS — Phase 6: Herdr socket client (event-driven acquisition)

Supersedes the completed Phase 5/5.5 plan (git history / PROGRESS.md). Authoritative for
BRIEF.md §14 Phase 6. Exit criteria: **normal monitoring uses the event-driven socket
client and survives Herdr client detach/reattach and watcher reconnects; polling fallback
retained.** Completes BACKLOG 9 / review.md finding 8 and BRIEF §8.3 (durable pane
identity via terminal_id).

Gate per commit: `go build ./... && go vet ./... && go test ./... -race -count=1`
(Go at ~/.local/go/bin, GOCACHE=/tmp/herdr-go-cache). Branch `phase-6-socket-client`.
Deployed-watcher safety: `--transport` defaults to **cli** this whole phase; zero flag
removals; store schema stays version 1 with one additive field; CLI-transport behavior
byte-identical. Never deploy mid-branch.

## Appendix A — Wire-protocol ground truth (herdr 0.7.5, protocol 17, probed live 2026-07-31)

- **A.1 Framing:** newline-delimited JSON, one object per line, both directions. No
  handshake, no auth (socket file mode 600 is the boundary). Socket:
  ~/.config/herdr/herdr.sock.
- **A.2 CRITICAL — one request per connection (probed):** server answers the first
  request then CLOSES the conn. Sequential reuse → EOF; pipelining → ECONNRESET.
  Persistent multiplexed request conns are IMPOSSIBLE on 0.7.5. Transport =
  **dial-per-request** (unix connect ~µs; still beats fork/exec+CLI parse). id echoed
  1:1 — verify echo.
- **A.3 Exception — events.subscribe (probed):** fresh conn, subscribe → ack
  `{"id","result":{"type":"subscription_started"}}` → conn STAYS OPEN streaming event
  frames. No further requests on it. Architecture: one long-lived events conn +
  dial-per-request for everything else.
- **A.4 Envelopes:** request `{"id","method","params"}`; success
  `{"id","result":{"type",…}}`; error `{"id","error":{code,message}}` (existing parse.go
  decode works; add id-echo check). Pushed events `{"event":kind,"data":{…}}` — NO id
  (the demux rule). Two kind vocabularies: dot form (pane.output_matched,
  pane.agent_status_changed — probed arriving dot-form) and underscore form
  (pane_created, pane_moved, layout_updated …) — DECODE BOTH (probe P2 pins).
- **A.5 Results:** `ping` → `{type:"pong",version:"0.7.5",protocol:17,…}` (protocol
  negotiation source). `session.snapshot` → snapshot{workspaces,tabs,panes,agents};
  PaneInfo has pane_id, **terminal_id**, workspace_id, tab_id, agent_status, revision,
  optional agent, agent_session{…}, cwd, terminal_title. `pane.list {workspace_id?}` →
  panes:[PaneInfo] (terminal_id ALSO available to the CLI adapter's decode).
  `pane.read {pane_id,source,lines?,strip_ansi=true}` → `{type:"pane_read",
  read:{text,revision,truncated}}` — socket wraps text in an envelope (CLI printed raw);
  extract read.text. `pane.process_info` → same foreground_processes shape as CLI.
  `pane.send_text {pane_id,text}` / `pane.send_keys {pane_id,keys:[...]}` → {type:"ok"}
  (keep esc/enter names). `notification.show {title,body?}`.
- **A.6 Subscriptions:** `pane.output_matched` REQUIRES pane_id, source,
  match{type:"substring"|"regex",value}; optional lines, strip_ansi; event data carries
  matched_line + FULL read (PaneReadResult). `pane.agent_status_changed` requires
  pane_id, optional agent_status filter. Global (no pane_id): pane.created/closed/
  updated/focused/moved/exited/agent_detected, layout.updated, workspace.*, tab.*.
  `pane_moved` data: {previous_pane_id, previous_workspace_id, previous_tab_id,
  pane:PaneInfo} — **pane IDs change on move; terminal_id is the durable key** (§8.3).
- **A.7 output_matched semantics (probed):** fires immediately AT SUBSCRIBE TIME when
  current content already matches (revision 0, ms latency); did NOT refire during 8s of
  redraw with a persistently-matching screen. Treat as edge/at-subscribe triggered;
  refire-on-reappearance UNVERIFIED (probe P1). Consequence: subscribe-then-poll has no
  acquisition gap; periodic subscription recycling re-arms level-triggered.
- **A.8 Socket path:** resolution order (docs): --session → HERDR_SOCKET_PATH →
  HERDR_SESSION → default. Our socket client takes ONLY an explicit configured path or
  the default — it must NOT read HERDR_* env (same inherited-session hazard the CLI
  scrub kills). `--transport socket --session x` without `--socket` → hard flag error
  this phase.
- **A.9 Live probes for orchestrator (throwaway; none block commit 1):**
  P1 refire (subscribe substring, match, clear, match again → refire without
  resubscribe?); P2 lifecycle envelope form + data shapes (create/move/close scratch
  pane while subscribed); P3 detach/reattach herdr client while subscribed (expect no
  disruption); P4 send-keys names via live E2E; P5 idle-hours timeout (soak answers).

## Design decisions

- **D-P6-1:** internal/runtime/herdr gains socket.go (dial-per-request Runtime impl) +
  events.go (long-lived subscription conn + reconnect). CLI adapter untouched except
  parse.go gaining terminal_id. Core packages never import the concrete adapter (arch
  test).
- **D-P6-2:** `run`/`doctor` gain `--transport cli|socket`, default **cli**. Default
  flip is post-soak, NOT this phase.
- **D-P6-3:** events are a capability, not a Runtime change: new
  internal/runtime/events.go defines the neutral model; socket adapter implements;
  runcmd type-asserts. Runtime interface unchanged (tmux/CLI/fake need no stubs).
  EventKind: output_matched, agent_status, pane_moved, pane_closed, panes_changed
  (created/updated/layout coalesced), resync (reconnect bootstrap done, carries
  Snapshot []Pane). EventSource{StartEvents(ctx, SubscribeSpec) (<-chan Event, error);
  UpdateSubscribedPanes([]string)}. SubscribeSpec{PaneIDs, MatchRegex, ReadSource,
  ReadLines}.
- **D-P6-4 socket internals:** SocketOptions{Path ("" → default), DialTimeout 3s,
  OpTimeout 5s (parity with CLI commandTimeout)}. call(): dial unix, SetDeadline,
  write one line, read one line (bufio 4MB cap), close; error envelope → HerdrError;
  id mismatch → error. Ping()/Snapshot() exposed for doctor+bootstrap. Every call
  deadline-bounded — never blocks the coordinator.
- **D-P6-5 threading:** events goroutine only decodes + sends on a buffered channel
  (cap ~64) with coalescing — trigger events collapse to latest per pane; NEVER block;
  never drop resync/pane_moved (retry-with-deadline, force-coalesce triggers first).
  ALL consumption on the run-loop goroutine — no locks in coordinator/jobs, no
  concurrent Tick/Reconcile.
- **D-P6-6 reconnect:** on stream error/EOF: backoff 500ms→15s ±20% jitter; each
  attempt = dial → ping (WARN if protocol≠17, continue) → session.snapshot →
  resubscribe → emit EventResync{Snapshot}. Resync handling in runcmd: SetPanes,
  manager.ReconcilePanes(snapshot), immediate action-capable poll + manager.Tick.
  Polling fallback retained (BRIEF §8.2 step 5).
- **D-P6-7 event-driven acquisition (the BACKLOG-9 fix):** per monitored pane subscribe
  pane.agent_status_changed (unfiltered) + pane.output_matched with source:"detection",
  lines:cfg.Lines, regex = conservative case-insensitive alternation over both
  providers' banner anchors (defined in runcmd wiring, NOT providers — server-side
  match is a trigger only; client Analyze stays authoritative; false trigger = one
  poll). On trigger: debounced (300ms quiescence, ≤1 forced fire/s) tick into the SAME
  detection channel the ticker feeds → Poll() → HandleLimit within ~1s of render,
  independent of --interval. Because A.7 refire is unverified: recycle events conn
  (full resubscribe re-runs subscribe-time matching) at most once per 60s AND only if
  a trigger fired since last recycle; min(interval,30s) ticker stays as unconditional
  net. Worst case = Phase 5.5 behavior.
- **D-P6-8 pane identity (§8.3):** store.Job += TerminalID (additive, schema 1; old
  binary rewriting the file drops it → degrade to legacy matching, acceptable).
  runtime.Pane += TerminalID. HandleLimit stamps it. Manager (flock-transactional):
  ReassignPane(prevPaneID, pane) on pane_moved — stored TerminalID equal → update
  PaneID/Workspace; different → MANUAL_REQUIRED ("pane identity changed"); empty
  legacy → update only if unique job for prev ID else MANUAL_REQUIRED.
  ReconcilePanes(panes) on resync — job's pane absent, exactly one snapshot pane with
  matching TerminalID → update; else leave for validate (SESSION_GONE/MANUAL as today).
  runcmd monitored-set: resolve --pane IDs to terminal IDs at startup; refresh filter
  accepts pane whose ID OR terminal ID matches (works for CLI transport too);
  UpdateSubscribedPanes recycles after ReassignPane IN THE SAME event handling.
- **D-P6-9 doctor socket mode:** connect; ping → protocol PASS(17)/WARN(other) from the
  REAL decoded value; snapshot decodes + protocol cross-check; subscribe layout.updated
  → subscription_started ack → close (round-trip). CLI-mode checks unchanged.
- **D-P6-10 test harness:** real unix listener in t.TempDir (short paths) reproducing
  ground truth: one-request-per-conn, close-after-response, RST on pipelining,
  scripted subscribe streaming, kill/restart for reconnect. net.Pipe NOT used — the
  real dial path is the coverage target.

## Commits

1. **Socket request transport (no wiring).** socket.go; parse.go (+terminal_id, socket
   result structs pong/snapshot/pane_read); runtime.go (Pane.TerminalID); herdr.go (CLI
   fills TerminalID; shared esc/enter key-map helper). Tests vs fake server: every
   Runtime method with verbatim param assertions; read.text extraction; HerdrError;
   id-mismatch error; no-response deadline (bounded, no hang); close-before-response
   wrapped error; Ping/Snapshot decode; HERDR_SOCKET_PATH env IGNORED (set in test,
   assert configured/default used).
2. **Event stream client + neutral model (no wiring).** internal/runtime/events.go
   (stdlib-only); herdr/events.go. Tests: subscribe request contents; ack + dot-form
   events decode; underscore-form pane_moved/created/layout frames decode (both forms
   until P2 pins); unknown kinds ignored; oversized frame (>64KB) survives; slow
   consumer never blocks reader (coalescing observed, pane_moved retained); server
   closes stream → backoff reconnect → re-ping+snapshot+resubscribe on second listener
   → EventResync with panes; ctx cancel joins goroutine (race gate = leak detector);
   UpdateSubscribedPanes recycles.
3. **Run-loop wiring.** runcmd.go: --transport flag (+validation incl. --session
   rejection); socket construction; event pump goroutine → debounced fires into the
   SAME detectionTicks channel (coordinator/loop.go UNCHANGED); resync → SetPanes +
   immediate poll + manager.Tick; recycle timer per D-P6-7. Tests: **BACKLOG-9
   regression** (banner present, event injected, ticker never fires → exactly one job;
   content stale after → no second job); event burst 20/100ms → ≤2 polls; status ticks
   survive event flood; resync path; transport selection + default-cli parity;
   --transport socket --session rejection.
4. **Pane identity (§8.3).** store.Job.TerminalID; manager HandleLimit stamp +
   ReassignPane + ReconcilePanes; runcmd pane_moved wiring + terminal-ID monitored-set
   + UpdateSubscribedPanes ordering; jobscmd inspect prints terminal_id. Tests:
   move-with-match updates under flock + survives external mtime reload; mismatch →
   MANUAL_REQUIRED; legacy unique/ambiguous; resync absent-then-found; pre-Phase-6
   schema-1 file loads + round-trips; moved monitored pane keeps polling; arch green.
5. **Doctor socket mode + docs.** doctorcmd.go (+--transport, injected dialer);
   README; docs/herdr-api.md (Appendix A wire model + probe results); PROGRESS.md;
   BACKLOG.md (close 9 pending live drill; add default-flip-after-soak); PLANS.md
   stays. Tests: doctor socket all-PASS vs fake; connect-refused FAIL; protocol 16 →
   WARN; missing subscription_started → FAIL; CLI-mode output byte-identical.
6. **(Orchestrator)** probes P1–P3, live E2E drills, soak decision, PROGRESS record.

## Live E2E (orchestrator)

1. doctor --transport socket all PASS. 2. Probes P1–P3 (record in docs/herdr-api.md;
adjust recycle if P1 shows refire). 3. **BACKLOG-9 live regression ×2:** scratch cat
pane + Claude chrome; watcher --transport socket --interval 10m --dry-run; paste banner,
bury it ~2s later → durable WAITING job within seconds; then non-dry short-reset cycle →
one esc/continue/enter → RESUMED attempts=1. 4. Detach/reattach herdr client
mid-WAITING → no churn, on-schedule resume. 5. Watcher restart mid-WAITING + the
RESUMING-at-restart drill. 6. Pane-move drill (terminal_id update; ambiguity →
MANUAL_REQUIRED). 7. Server-restart reconnect: covered by fake-server tests (production
server hosts live panes — do not restart it). 8. Negative: w7:p1 socket dry-run 3min →
zero jobs. 9. **Soak:** wD:p1 stays cli; parallel scratch soak watcher on socket
24–48h; if clean, switch wD:p1 to --transport socket explicitly (default flip is a
later one-liner).

## Risks

1. Never assume conn reuse — pipelining RSTs; harness encodes it so future
   "optimizations" fail tests.
2. Lifecycle envelope form unverified — decode both; P2 pins before commit 4 relies on
   pane_moved.
3. output_matched refire unknown — events are NEVER the sole path; ticker + at-subscribe
   matching + recycle bound the window regardless.
4. Event goroutine must not touch coordinator/jobs state — channel-only; race gate
   enforces.
5. Event frames embed full reads — 4MB reader cap + oversized-frame test.
6. Old-binary rewrite drops terminal_id — degrade-to-legacy designed in; never make it
   mandatory in validate.
7. ReassignPane → UpdateSubscribedPanes ordering in the same handler; test it.
8. Deploy discipline: wD:p1 keeps cli until E2E step 9.
9. Startup ordering forgiving (at-subscribe matching); still subscribe first; episode
   fingerprint dedupes double-trigger.
10. --session+socket unsupported this phase (flag-parse error, documented).

## Later (not this phase)

Default transport flip post-soak; agent_session identity signal in validate;
pane.wait_for_output verification acceleration; YAML config with Phase 7 packaging.
