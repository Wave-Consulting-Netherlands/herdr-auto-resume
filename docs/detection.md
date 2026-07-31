# Claude detection fixtures

Phase 4 keeps terminal evidence conservative. `internal/detection` first strips terminal
control sequences and inspects a bounded tail, then pairs a limit line with a reset line
within six lines. Quoted tool output, fenced code, `> ` quotes, and stale output below a
banner are not actionable. A clear Claude wait/stop menu is visible for diagnostics but
still requires manual validation before input is sent.

Use the read-only diagnostic against a captured pane:

```text
herdr-auto-resume detect --file internal/detection/testdata/claude/positive/cc2026-07_menu-example.txt
```

Fixtures are versioned by Claude Code and Herdr capture date. Positive fixtures must have
a family, reset kind, and parsed time. Negative fixtures may contain quoted limit text,
but must never produce actionable analysis. Raw captures remain outside the repository;
paths and project names in committed fixtures are sanitized to `/home/user` and
`example-project`.
