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

## Test results

- 2026-07-30, upstream import at `39ad5ef`: `go build ./...` OK; `go test ./...` — all
  detection tests pass (only package with tests upstream). Toolchain go1.26.5 linux/arm64.

## Next task

Phase 0 Step 0.4: module rename commit (`go.mod` → `github.com/walt-verweij/herdr-auto-resume`;
fix imports in `main.go`, `internal/tui/tui.go`, `internal/tui/layout.go`), then verify gate
(`go build ./... && go vet ./... && go test ./...` + `go test -race ./...`), merge
`phase-0-bootstrap` → `master`, push `master` + tag to origin. Then Phase 1 Step 1.1
(`internal/runtime` package) per the approved plan.
