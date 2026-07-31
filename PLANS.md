# PLANS — Phase 7: Packaging, config, single-instance guard, TUI/plugin evaluation (FINAL)

Supersedes the completed Phase 6 plan. Authoritative for BRIEF.md §14 Phase 7. Exit
criteria: **documented installation and upgrade path; no manual process management
required for normal use.** Closes BACKLOG 2 and 4; audits BRIEF §20.

Gate per commit: `go build ./... && go vet ./... && go test ./... -race -count=1`
(Go 1.26.5 at ~/.local/go/bin, GOCACHE=/tmp/herdr-go-cache,
GOMODCACHE=/tmp/herdr-go-mod-cache). Branch `phase-7-packaging`. Deployed watchers
(wD:p1 cli, wQ:p1 soak socket, separate state files) keep working all phase; no store
schema changes; no detection/job behavior changes beyond the run lock.

## Decisions

- **D-P7-1 Config file: IN, minimal read-only YAML (BRIEF §11).** internal/config.
  Rationale: last phase — §11 must not stay unconformed; systemd units need a stable
  ExecStart (edit config → restart, never unit edits); absent file ⇒ byte-identical
  behavior (parity regression test mandatory). Scope: read-only; flat mapping of
  EXISTING run flags; `version: 1` required; unknown keys rejected
  (yaml KnownFields(true)); precedence built-in defaults < config file < explicitly-set
  flags (flag.FlagSet.Visit tracks set flags — including a flag explicitly set to its
  default value, which still wins). Path ~/.config/herdr-auto-resume/config.yaml,
  `--config` override (explicit missing path = error; default missing = fine; honors
  XDG_CONFIG_HOME). New dependency: gopkg.in/yaml.v3 (the phase's only one).
- **D-P7-2 First release = v0.2.0, NOT v0.1.0.** Upstream autoclaude tags
  v0.1.0–v0.1.2 exist and are pushed on origin (ancestors of HEAD). Never touch them;
  v0.2.0 is a minor bump per §18 (config changes ⇒ minor).
- **D-P7-3 Plugin: defer.** Live `herdr plugin --help` (0.7.5) shows install/link/
  enable/list/config-dir/action invoke/log/plugin-owned panes — §8.4's benefits could
  wrap the existing binary later, but no startup/event-hook contract beyond what the
  socket API gives, no schema methods, moving 0.7.x target. §21 decision stands.
  Record capability inventory + wrap-the-binary sketch in docs/packaging.md.
- **D-P7-4 Herdr-native TUI: defer.** §12: daemon+CLI outrank TUI; status/inspect/
  doctor + herdr's own views cover the display list. tmux Bubble Tea TUI stays as-is
  (upstream parity). Record rationale.
- **D-P7-5 Single-instance run lock (live footgun: a soak watcher defaulted into the
  production state file).** Exclusive non-blocking flock on `<abs(statePath)>.run`
  sidecar, acquired once at run startup after resolveStatePath, held for the whole run
  (fd open). Second instance same state file → fail fast exit 1 with holder PID + hint
  to use a different --state-file. `--state-file off` ⇒ no lock. Different state files
  ⇒ different locks (wD/wQ setup untouched). Deliberately NOT the transactional store
  .lock (short-lived, CLI commands take it) — separate .run file so status/cancel work
  while a watcher runs. No unlink on release (unlink+flock race trap; stale empty file
  harmless).
- **D-P7-6 Fold-ins: BACKLOG 2 only.** writeJobStatus already calls .Local() but host
  TZ is UTC so unproven — thread a *time.Location (default time.Local), pin with a
  Europe/Amsterdam regression. BACKLOG 1/6/7/10 stay out (noted deliberately).
- **D-P7-7 Two first-class run modes:** (1) inside a herdr pane by hand (today's
  wD:p1); (2) systemd user service / launchd example. Unit: explicit --transport
  (cli today; post-soak socket = one word), Restart=on-failure, restart-loops until
  herdr reachable (herdr not systemd-managed here; After=herdr.service shown
  commented), Environment=PATH=%h/.local/bin:… (user units lack ~/.local/bin),
  NoNewPrivileges. Docs: `loginctl enable-linger` MANDATORY on headless hosts (this
  host has Linger=no).

## Commits

1. **Release identity + version provenance (BACKLOG 4).**
   - main.go: add `var commit = "none"`, `var date = "unknown"`; `version` prints
     `herdr-auto-resume <version> (commit <commit>, built <date>, <go version>)`;
     version=="dev" falls back to runtime/debug.ReadBuildInfo vcs.revision/vcs.time.
   - doctorcmd.go: first line `INFO version: herdr-auto-resume <version> (<commit>)`
     (both transports; §18 doctor-in-bug-reports).
   - .goreleaser.yml: project_name herdr-auto-resume; builds[0].binary
     herdr-auto-resume; ldflags `-s -w -X main.version={{.Version}} -X
     main.commit={{.ShortCommit}} -X main.date={{.Date}}`; drop homebrew comment.
   - .github/workflows/ci.yml: go-version-file go.mod; add release-dry-run job
     (goreleaser-action ~> v2: `check` then `release --snapshot --clean
     --skip=publish`, fetch-depth 0) — goreleaser is NOT installed locally; CI is the
     validation.
   - .github/workflows/release.yml: go-version-file go.mod; remove
     HOMEBREW_TAP_GITHUB_TOKEN.
   - scripts/release.sh: REPO=Wave-Consulting-Netherlands/herdr-auto-resume; delete
     homebrew-tap section; keep tag-guard → tag → push → gh run watch → checksums.
   - README.md: fix CI badge to org repo (restructure waits for commit 5).
   Tests: version output (name, injected values, dev fallback no-panic); doctor
   version-line-first with otherwise byte-identical output (both transports).
2. **Single-instance run lock (D-P7-5, TDD).**
   - internal/store/runlock.go: AcquireRunLock(statePath) (*RunLock, error) —
     MkdirAll 0700, open `<abs>.run` O_RDWR|O_CREATE 0600, Flock LOCK_EX|LOCK_NB;
     EWOULDBLOCK → read stored PID → error "state file %s is already in use by
     herdr-auto-resume run (pid %s); use a different --state-file for a second
     watcher"; success → truncate+write own PID; Release() closes fd.
   - runcmd.go: after resolveStatePath, if statePath != "off" acquire; error → print +
     exit 1; defer Release.
   - doctorcmd.go: NB-probe the resolved .run lock → `INFO watcher: active (pid N) on
     <path>` / `INFO watcher: none on <path>`; release probe immediately.
   Tests: second acquire same path fails w/ path+pid (flock conflicts across separate
   open descriptions in-process — no subprocess needed); different paths both succeed;
   release-then-reacquire; PID written; runCommand exits 1 before runtime construction
   when pre-held; `--state-file off` touches no lock; doctor INFO lines.
3. **Minimal YAML config (D-P7-1, TDD).**
   - go.mod/go.sum: gopkg.in/yaml.v3.
   - internal/config/config.go + validate.go: Load(path) (Config, found bool, error).
     Schema (all optional except version: 1):
     runtime{type herdr, transport cli, herdr_bin, socket, workspace};
     monitoring{panes [], interval 3s, lines 200};
     resume{margin 60s, max_wait 192h, verify_timeout 90s};
     providers{enabled [claude codex], claude_prompt, codex_prompt};
     state{file auto}. Strict decode, durations via ParseDuration, ~ expansion for
     socket/state.file. DefaultPath honors XDG_CONFIG_HOME.
   - runcmd.go: --config flag; merge defaults → config → explicitly-set flags
     (fs.Visit); ALL existing validation runs on the merged result (pane requirement
     satisfiable from monitoring.panes — makes the systemd unit generic).
   - jobscmd.go: job commands' --state-file default comes from config state.file when
     resolvable (fixes wrong-state-file status confusion).
   - doctorcmd.go: config check — absent default `INFO config: none`; valid `PASS
     config: <path>`; invalid `FAIL` with the validation error.
   Tests: full parse; unknown key rejected naming the key; version missing/wrong;
   bad duration/transport/provider; tilde expansion; absent default = zero+not-found;
   precedence matrix incl. flag-set-to-default-value wins; panes-from-config satisfies
   requirement; --config missing explicit path errors; **absent config ⇒ parseRunFlags
   deep-equals today's output (deployment parity)**. go mod tidy clean.
4. **BACKLOG 2 + packaging assets.**
   - jobscmd.go: writeJobStatus(out, jobs, loc *time.Location); caller passes
     time.Local; Europe/Amsterdam regression proves local rendering. Close BACKLOG 2.
   - packaging/systemd/herdr-auto-resume.service per D-P7-7 (Type=exec; ExecStart
     `%h/.local/bin/herdr-auto-resume run --config %h/.config/herdr-auto-resume/
     config.yaml --transport cli`; Environment=PATH=%h/.local/bin:/usr/local/bin:
     /usr/bin:/bin; Restart=on-failure; RestartSec=5s; NoNewPrivileges=yes;
     StartLimitIntervalSec=0; WantedBy=default.target; commented After/Wants
     herdr.service).
   - packaging/launchd/nl.wave-consulting.herdr-auto-resume.plist: ProgramArguments
     […run --config … --transport cli], RunAtLoad, KeepAlive{SuccessfulExit=false},
     Std*Path under ~/Library/Logs; marked example-only/untested.
   - packaging/config.example.yaml: full commented schema.
   Verify: gate; `systemd-analyze --user verify` non-gating; plistlib parse check.
5. **README restructure + docs + conformance audit.**
   - README: Install (release tarballs, go install, source), Quickstart, Running it
     (pane mode AND systemd/launchd, linger note), Operations (status/inspect/cancel/
     detect/doctor, multi-watcher state isolation + run lock), Configuration reference
     (all flags + config schema + precedence), Upgrade (binary replace → restart;
     semver policy per §18; tested herdr/CC/Codex versions per release),
     Troubleshooting (doctor first, run-lock error meaning). Keep fork notice + tmux
     TUI section.
   - docs/packaging.md: plugin inventory (D-P7-3), TUI deferral (D-P7-4), release
     flow, launchd caveat.
   - PROGRESS.md: Phase 7 record + D-P7 decisions + **BRIEF §20 audit**: met 1,2,4-17
     (evidence per phase records); met-with-deviation 3 (strict opt-in, no runtime
     enable/disable verbs — disable = restart without the pane; intentional);
     deferred: herdr TUI, plugin packaging, §12 `test` subcommand (covered by
     --test-pattern + --dry-run), §11 logging/notifications config sections.
   - BACKLOG.md: close 2 and 4; annotate 5 out-of-repo; 1/6/7/10 kept deliberately;
     fix duplicate item numbering.
6. **(Orchestrator)** L1 doctor both transports (version/config/watcher lines);
   L2 run-lock live drill vs the running wD watcher (default state → fail-fast w/ PID;
   /tmp state → starts); L3 config-parity restart of wD (+ optional systemd migration:
   enable-linger, unit, verify); L4 merge + CI green incl. release-dry-run;
   L5 release: ./scripts/release.sh 0.2.0 → tag → CI release → verify linux_arm64
   asset checksum + `version` output → install → restart watchers; L6 PROGRESS release
   record (tested versions: herdr 0.7.5/proto 17 + CC/Codex versions in use).

## Risks

1. Goreleaser validated only in CI (absent locally) — release-dry-run job lands on
   master BEFORE tagging; failed tag release = delete only v0.2.0, retry.
2. Never touch v0.0.x/v0.1.x upstream tags.
3. Old/new binary mix during rollout takes no cross lock (old predates .run) —
   accepted; restart both watchers promptly; documented.
4. Config precedence regressions — absent-file parity test mandatory; deployment gets
   no config file until L3.
5. fs.Visit walks only SET flags — the set-to-default-value case is pinned in tests.
6. systemd user-unit PATH restriction — unit ships Environment=PATH; docs prefer
   absolute --herdr-bin; linger required (host has Linger=no).
7. yaml.v3 maintenance mode — accepted (tiny strict surface).
8. BACKLOG.md has duplicate item numbers — renumber while editing.
