# PROGRESS

## Upstream provenance

Fork of [`henryaj/autoclaude`](https://github.com/henryaj/autoclaude) at commit
`39ad5ef1818a9c71241bea463da3af33f1dccf69` ("Handle SIGINT/SIGTERM for clean ctrl-c exit",
branch `master`, tagged locally as `upstream-39ad5ef`).

Upstream is MIT licensed, "Copyright (c) 2025 Henry Stanley". The `LICENSE` file is
preserved unmodified. This fork retains upstream attribution per BRIEF.md §6.4 and §20.17.

Fork remote: `https://github.com/walt-verweij/herdr-auto-resume` (origin);
`https://github.com/henryaj/autoclaude` (upstream).

## Completed

- **Phase 0 (in progress)** — 2026-07-30
  - Go 1.26.5 installed to `~/.local/go` (linux-arm64; host had no Go toolchain).
  - GitHub fork created: `walt-verweij/herdr-auto-resume`.
  - Repo initialized in `/home/ubuntu/dev/Herdr-auto-resume`; `master` checked out from
    `upstream/master` at `39ad5ef`; upstream tests green on import
    (`go build ./...` + `go test ./...`, detection package 55 assertions).
  - Branch `phase-0-bootstrap`: committed `BRIEF.md`, this file, README fork notice.
- **Phase 1 complete** — 2026-07-30
  - Extracted the tmux-independent runtime interface, geometry, fake runtime, and resume
    coordinator; moved tmux behind its runtime adapter; rewired the TUI and CLI dry-run
    support.
- **Phase 2 complete** — 2026-07-30
  - Added CLI subcommand dispatch, the fixture-driven Herdr CLI runtime adapter, headless
    run loop/command, and ordered doctor diagnostics.

## Design decisions

- **Module rename in Phase 0** to `github.com/walt-verweij/herdr-auto-resume`, before any
  new packages exist, so the rename touches only `go.mod` + 3 import sites instead of every
  later file. Note: `gh auth status` displays the account alias `wave-consulting-nl`, but
  the actual GitHub login (and thus module path owner) is `walt-verweij`.
- `.goreleaser.yml` / CI workflows left untouched this phase; the goreleaser binary name
  still says `autoclaude`. To be revisited in the packaging phase (BRIEF.md Phase 7).
- Scope of current work order: BRIEF.md Phases 0–2 only (bootstrap, runtime abstraction
  over tmux, Herdr CLI adapter with dry-run). No Codex provider, no socket client, no
  persistence/state-machine (Phase 3), no plugin.
- Runtime interface intentionally has no `Subscribe` method; Phase 1 remains polling-based
  and keeps the coordinator independent of any concrete adapter.
- `SendText` delegates to plain tmux `send-keys` without `-l`, preserving upstream behavior.
- The tmux adapter ignores `ReadPane`'s `lines` argument because upstream captures the
  visible viewport.
- Window pinning moved into `tmux.New`, which calls `CurrentWindowID` once at startup.
- Runtime pane descriptors are separate from coordinator-owned pane state.
- Herdr child processes scrub every `HERDR_*` variable inherited from the parent; only an
  explicitly configured `SocketPath` is re-added. This avoids the inherited socket hazard,
  where a child could accidentally target the controller's own Herdr session.
- Headless `run` requires strict `--pane` opt-in. This prefers false negatives over
  accidentally sending input to an unselected pane.
- Herdr pane reads use `--source recent` and consume plain text directly; pane reads do not
  decode the JSON envelope used by the other CLI commands.
- Herdr command failures first decode the JSON error envelope and otherwise preserve the
  command failure for callers to report.

## Test results

- 2026-07-30, upstream import at `39ad5ef`: `go build ./...` OK; `go test ./...` — all
  detection tests pass (only package with tests upstream). Toolchain go1.26.5 linux/arm64.
- 2026-07-30, Phase 1 final `go test ./...`:

  ```text
  ?    github.com/walt-verweij/herdr-auto-resume [no test files]
  ok   github.com/walt-verweij/herdr-auto-resume/internal/arch (cached)
  ok   github.com/walt-verweij/herdr-auto-resume/internal/coordinator (cached)
  ok   github.com/walt-verweij/herdr-auto-resume/internal/detection (cached)
  ok   github.com/walt-verweij/herdr-auto-resume/internal/runtime (cached)
  ok   github.com/walt-verweij/herdr-auto-resume/internal/runtime/tmux (cached)
  ?    github.com/walt-verweij/herdr-auto-resume/internal/tui [no test files]
  ```

- 2026-07-30, Phase 2 final `go test ./...`:

  ```text
  ok   github.com/walt-verweij/herdr-auto-resume (cached)
  ok   github.com/walt-verweij/herdr-auto-resume/internal/arch (cached)
  ok   github.com/walt-verweij/herdr-auto-resume/internal/coordinator (cached)
  ok   github.com/walt-verweij/herdr-auto-resume/internal/detection (cached)
  ok   github.com/walt-verweij/herdr-auto-resume/internal/runtime (cached)
  ok   github.com/walt-verweij/herdr-auto-resume/internal/runtime/herdr (cached)
  ok   github.com/walt-verweij/herdr-auto-resume/internal/runtime/tmux (cached)
  ?    github.com/walt-verweij/herdr-auto-resume/internal/tui [no test files]
  ```

## Next task

BRIEF.md Phase 3 — persistent scheduler and safety gates (state machine, atomic JSON store) — not in current scope
