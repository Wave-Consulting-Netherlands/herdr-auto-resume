# PLANS — Phase 4: Claude Code production support

Supersedes the completed Phase 3 plan (see git history / PROGRESS.md). Authoritative for
BRIEF.md §14 Phase 4. Exit criteria: **all committed positive Claude fixtures detected;
all committed negative fixtures never trigger; an end-to-end simulated reset resumes.**

Out of scope: menu navigation, Codex provider, socket client, provider-interface
extraction, API-overload/backoff handling.

Gate per commit: `go build ./... && go vet ./... && go test ./... -race -count=1`
(Go at `~/.local/go/bin`). Branch: `phase-4-claude-production`. The deployed watcher
(pane wD:p1, schema-1 state file) must keep working: no flag removals, additive store
fields only, detection default changes strictly-safer.

## Design decisions

- **D-P4-1:** new `internal/terminal` = standalone stdlib-only leaf (ANSI/CSI/OSC/DCS
  stripping, line handling, bounded tail). `detection` imports `terminal`; `jobs` keeps
  consuming `detection` predicates. Dependency direction `jobs → detection → terminal`.
  Arch test walks `../terminal` and asserts stdlib-only.
- **D-P4-2:** clock injection = explicit `now time.Time` parameter (carries Location →
  injects clock AND local zone; no globals). New API `detection.Analyze(content, now)`;
  `CheckRateLimitAt(content, now)`; `CheckRateLimit` stays as a `time.Now()` wrapper so
  the TUI path compiles untouched.
- **D-P4-3:** menu visibility does NOT block job creation (banner+menu is a genuine
  limit with parseable reset — see fixture from `example`); it blocks SENDING (legacy
  immediate-send path skips; validation gate 9 → MANUAL_REQUIRED).
- **D-P4-4:** `Analysis.Actionable` (live-tail/stale/quoted guard) gates LimitEvent
  emission and the 15-min periodic path; raw `IsLimited` keeps feeding TUI display.
- **D-P4-5:** evidence hash stays sha256(raw content) — deployed dedupe keeps matching.
- **D-P4-6:** `_ "time/tzdata"` imported in main.go (tz database fallback).

## Types (sketch)

```go
// internal/terminal
func StripANSI(s string) string  // CSI incl private-mode, OSC(+BEL/ST), DCS, APC/SOS/PM,
                                 // C0 except \n \t; \r\n→\n, bare \r dropped; \n preserved
func Lines(s string) []string    // strip + split, keeps empty lines positionally
func Tail(lines []string, n int) []string
func TailString(content string, n int) string

// internal/detection resetspec.go
type ResetKind string   // absolute | local-clock | relative | date-time | unknown
type Confidence string  // high | medium | low
type ResetSpec struct { Kind ResetKind; Raw, Timezone string; ParsedTime time.Time; Confidence Confidence }
func ParseReset(text string, now time.Time) ResetSpec

// internal/detection families.go / live.go
type Family string // n-hour-limit session-limit weekly-limit usage-limit extra-usage
                   // hit-limit limit-reached relative-retry ""
type Analysis struct { IsLimited, Actionable, MenuVisible bool; Family Family;
                       Reset ResetSpec; Evidence string }
func Analyze(content string, now time.Time) Analysis
func HasRateLimitMenu(content string) bool
// RateLimitStatus gains additive field Spec ResetSpec

// internal/store Job additive fields (schema version stays 1):
ResetKind, ResetTimezone, Confidence string // json omitempty
```

## Parsing rules (each = test case; all fake-clock)

- Clock+am/pm → local-clock in now.Location() unless tz follows.
- Clock+`(IANA)` → LoadLocation → absolute/high. **Fixes live bug: printed tz currently
  ignored, time parsed host-local.**
- Clock+abbreviation: fixed map UTC/GMT/BST/CET/CEST/EST/EDT/ET/PST/PDT/PT/CST/CDT/CT/
  MST/MDT/MT → IANA, medium confidence; unknown abbr → local-clock/low, keep raw tz.
- 24h clock (`15:30`) unambiguous; 1–12 no am/pm → soonest-future, medium.
- Passed clock ≤1h grace → keep today; older → NEXT occurrence via date-anchored
  time.Date (never Add(24h) — DST trap).
- DST deterministic late-side: spring-forward gap → Go normalizes forward (accept);
  fall-back repeated hour → if candidate.Add(1h) renders same wall clock take the LATER
  instant. Tests both 2026 transitions in Europe/Amsterdam + one US zone.
- Relative: `8m`, `45m`, `in 2 hours`, `in: 3 hours`, `try again in 5 hours`,
  `wait 30 mins`, `2h 30m`.
- Date-time (weekly): `Oct 9, 10am`, `Oct 9 at 10am (Europe/Amsterdam)`,
  `Thursday 3pm`/`Thu 3pm` (next occurrence, today-if-future); year rollover.
  High confidence with explicit date, medium weekday-only.
  (Reference parser claude-auto-retry does NOT support date-time — we deliberately do.)
- Implausible: hour>23, minute>59, `resets 30`, >370 days out → unknown/zero time.
- Final resume base stored UTC; Raw + Timezone retained. Margin/horizon stay in jobs.
- Host-tz independence: parse derives location ONLY from now.Location(); test with a
  non-host zone.

## Detection mechanics (port from claude-auto-retry; see appendix patterns)

- LIMIT line + RESET line paired within 6 lines; bottom-most reset wins.
- Chrome-aware content tail: strip trailing chrome lines — blank, box rules, boxed input
  `│ > … │`, bare `❯`/`>` prompt row, `⏵⏵` footer, `? for shortcuts`, `| vX.Y.Z`,
  usage footer `[Opus … | Max] …`, tool tallies `✓ Bash ×10 | …`,
  `✓ All todos complete (N/N)`, `□◼✓` todo items, `N tasks (`, `+N completed`,
  spinner `✻ …`, `Opening your options…`, `╌╌╌`/`───` — every classifier
  render-anchored, each with a prose-probe negative test (`Press ctrl+c to stop the dev
  server`, `✓ Fixed the bug`, `Released v0.5.1`, psql `│ … │` table rows survive).
- Stale-scrollback guard: non-chrome non-menu output BELOW the banner → Actionable=false.
- Quoted protection: tool-echo mask (`⏺`/`●`/`∙` `Name(` headers + their `⎿`/`└`/indented
  children, computed over full read then sliced), fenced code blocks, `> ` quotes.
  **Load-bearing exception:** a `⎿` banner NOT governed by a `Name(` header is LIVE
  (Claude renders the real banner as a `⎿` child of an interrupted tool call — pinned by
  the `example` fixture).
- Menu: `What do you want to do?` + option lines `^\s*❯?\s*\d+\.\s` + (`Stop and wait
  for limit to reset` | `Enter to confirm` | `Esc to cancel`), live tail only.
- `IsIdlePrompt` refinement (closes BACKLOG 3): bare `❯` prompt glyph no longer vetoes;
  only menu-shaped `❯ N.` lines / menu blocks do.

## Commits

1. **internal/terminal** + arch-test extension + detection.StripANSI delegates +
   `_ "time/tzdata"` in main.go. Tests: CSI/private-mode/OSC-8/OSC-0/DCS/APC, line
   preservation, Tail bounds; existing detection tests stay green.
2. **Clock injection seam** (no behavior change): CheckRateLimitAt/HasResetAt; thread
   now through parseResetTime; coordinator (`c.clock()`) + jobs (`now`) migrate;
   characterization tests pin today's exact outputs (minutes format, −1h rollover,
   fallback empty ResetsAt) under fixed fake now.
3. **ResetSpec parser** (resetspec.go): full rules above; RateLimitStatus.Spec additive;
   clock/minutes paths rebuilt on ParseReset with legacy fields mapped identically.
   Table tests incl. Asia/Kolkata half-hour, Pacific/Auckland day-boundary, both DST
   transitions, grace window, 12am/12pm, year rollover, implausibles.
4. **Message families + positive corpus** (families.go + testdata/claude/positive/):
   families per the type list; widen CheckRateLimitAt regexes (session/weekly/usage/
   extra-usage/N-hour/try-again); every new pattern gets ≥1 negative probe. Corpus test
   walks positive dir asserting IsLimited+Family+Kind+ParsedTime per a filename-keyed
   table. Sanitize root example/example2 into corpus (originals stay). Fixture naming
   versioned (cc2026-07_*, CC 2.1.220, herdr 0.7.5).
5. **live.go: chrome/quote/stale guards + Analyze + negative corpus** + IsIdlePrompt
   refinement + coordinator migration (Analyze, LimitEvent.Spec, Actionable gating,
   menu-blocks-immediate-send). Negative fixtures: source_code, readme_quote,
   user_prompt, agent_analysis (sanitized from live wA:p1 capture), command_history,
   stale_scrollback_newer_output, test_output, non_claude_pane, code_fence_quote,
   tool_echo_grep, starship_prompt_tail. Negative walk asserts Actionable=false AND
   !(IsLimited && Actionable && nonzero ParsedTime). MUST land before commit 6.
6. **Jobs integration**: gate 9 via Analyze (MenuVisible replaces blanket ❯ check;
   delete manager.go hasMenuInTail); HandleLimit persists ResetKind/ResetTimezone/
   Confidence; store additive fields + old-file load test; notification body includes
   pane + human local reset time; log lines gain kind= confidence=; jobs regression
   tests (starship-tail idle pane now passes gate 9; menu fixture still MANUAL_REQUIRED);
   optionally fix BACKLOG 2 (status RESET column local time). inspect prints new fields.
7. **detect subcommand** (BRIEF §12: `detect --file fixture.txt` prints
   IsLimited/Actionable/MenuVisible/Family/Kind/Timezone/ParsedTime UTC+local/
   Confidence/Evidence; never sends) + docs + PROGRESS/BACKLOG updates.
   Live E2E (orchestrator, not Codex): weekly-format drill, timezone-format drill
   (Europe/Amsterdam banner on UTC-parsed clock), negative live drill (BRIEF-quoting
   pane → zero jobs), deployed-watcher upgrade check (schema-1 state file reconciles).

## Raw material

- Live captures in scratchpad `p4-fixtures/`: claude_working_chrome.txt (w1:p1),
  claude_idle_chrome.txt (w4:p1), agent_analysis_hazard.txt (wA:p1), codex_pane.txt
  (w6:p1). Sanitize before committing: paths → /home/user, project names →
  example-project, task text → same-shape filler; KEEP all glyphs/box-drawing/wording/
  indentation; grep for token|key|secret|Bearer|ssh- before commit. Versions: Claude
  Code 2.1.220, herdr 0.7.5 proto 17.

## Appendix — reference patterns (claude-auto-retry, MIT)

Limit lines: `/(?:hit|exceeded|reached).*(?:your|the)\s*(?:[\w-]+\s+){0,3}limit/i`,
`/\d+-hour limit/i`, `/limit reached/i`, `/usage limit/i`, `/out of.*usage/i`,
`/rate limit/i`, `/try again in/i`.
Reset lines: `/resets?\s+(?:at\s+)?\d{1,2}(?::\d{2})?\s*(?:am|pm)?/i`,
`/resets?\s+in[:\s]\s*\d/i`, `/try again in \d+\s*(?:hours?|minutes?|h|m)/i`.
Time extraction: `/resets?\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*(?:\(([^)]+)\))?/i`;
relative `/(?:try again|wait|resets?\s+in)[:\s]\s*(?:for\s+)?(?:in\s+)?(\d+)\s*(hours?|minutes?|mins?|h|m)\b/i`.
Message examples to cover as fixtures: `5-hour limit reached - resets 3pm (UTC)`,
`You've hit your limit · resets 3pm (Europe/Dublin)`, `You've hit your session limit ·
resets 2am (Europe/Zurich)`, `You've hit your weekly limit · resets 9am (Europe/London)`,
`You've hit your 5-hour limit · resets 3pm (UTC)`, `You've hit your weekly limit ·
resets Oct 9, 10am`, `Claude usage limit reached. Resets at 2pm`, `You're out of extra
usage · resets 3pm`, `Please try again in 5 hours`, `usage limit · resets in: 3 hours`,
`resets in 2 hours`, `wait 30 mins`, `Rate limit hit. Resets at 4pm`, multi-line
`⚠ You've hit your limit` / `· resets 3pm (UTC)`, menu block `What do you want to do?` /
`❯ 1. Upgrade your plan` / `2. Stop and wait for limit to reset` / `Enter to confirm ·
Esc to cancel`.

## Risks

1. Fall-back duplicate hour: time.Date picks earlier offset silently — explicit
   late-side check required or watcher wakes an hour early.
2. `⎿`-child live banner vs tool-echo mask: header-governed discipline is load-bearing;
   `example` fixture pins it.
3. Loose chrome regexes strip real work → stale banner pulled back in; prose-probe
   table mandatory.
4. Commit 4 widens regexes before commit 5's liveness guard: keep negative string
   probes in commit 4; do NOT deploy the binary to wD:p1 between commits 4 and 5.
5. Gate-9 relaxation only in commit 6, after menu detection proven in 5.
6. time.Local leakage: location only from now.Location(); non-host-zone test.
7. Periodic path now gated on Actionable — intended strictly-safer change; note in
   PROGRESS.
8. Abbreviation ambiguity (CST): fixed map only, medium confidence, never guess beyond.
