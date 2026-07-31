# Codebase Review

Reviewed `phase-5-codex-provider` at commit `2181bd1` on 2026-07-31.

## Scope and approach

The review covered all 35 production Go files, the 31 Go test files, provider
fixtures, CLI commands, runtime adapters, persistent job lifecycle, TUI
integration, documentation, and release/configuration files. Particular
attention went to the safety boundary around sending input to a pane, durable
exactly-once behavior, provider identification, reset parsing, and operational
failure reporting.

Baseline validation was green:

```text
go build ./...                  PASS
go vet ./...                    PASS
go test ./... -race -count=1    PASS
```

Five focused, deterministic probes were also run and then removed. They
confirmed findings 1, 2, 3, 9, and 10 below without leaving test or production-code
changes in the repository.

## Summary

No critical vulnerability was found. There are four high-severity reliability
findings, four medium-severity operational findings, and two low-severity
input/configuration findings. The strongest parts of the codebase are its
provider boundary, extensive positive/negative detection fixtures, explicit
durable job states, conservative restart reconciliation, atomic state-file
replacement, and race-enabled test gate.

The main weakness is that several safety decisions are locally correct but are
not enforced across boundaries: the CLI and watcher can overwrite each other,
the provider registry's authoritative agent-hint rule is not used during
delayed-job validation, and lower-level send failures are discarded or followed
by additional input.

## Findings

### 1. High — A concurrent cancellation can be overwritten and still send a resume

**Impact:** `jobs cancel` can print success while the watcher subsequently
overwrites `CANCELLED`, validates the stale in-memory job, and sends the resume
sequence. This violates the user's explicit cancellation and the exactly-once
safety model.

**Evidence:** `cancelJob` performs a whole-file load/modify/save with no lock or
compare-and-swap (`jobscmd.go:133-154`). The manager reloads only at the start of
a tick (`internal/jobs/manager.go:225-247`, `312-333`), then every transition
replaces the complete file from its in-memory copy
(`internal/jobs/manager.go:377-386`). A cancellation written while validation is
blocked is therefore lost on the next manager update.

**Validation:** A deterministic probe paused `ListPanes`, cancelled the job
through a second store handle, resumed validation, and observed both a sent
resume and a final non-cancelled state.

**Recommendation:** serialize every state-file transaction across watcher and
CLI processes with a file lock, and re-read plus compare the job's expected
state immediately before each transition and before sending input. Treat a
failed comparison as cancellation or concurrent modification and fail closed.

### 2. High — Resume actions continue after an earlier input operation fails

**Impact:** If Escape or text injection fails, the function still sends all
remaining operations, including Enter. The final Enter can activate whatever
currently has focus even though the intended prompt was not inserted, which is
the most hazardous failure mode for a pane-control tool.

**Evidence:** `SendResumeAction` records `firstErr` but does not return or stop
the sequence; it always proceeds through pre-keys, text, and submit key
(`internal/coordinator/coordinator.go:310-331`).

**Validation:** With `SendText` forced to fail, the deterministic probe observed
`Escape`, the failed text call, and then `Enter`.

**Recommendation:** return immediately on the first failed operation. Add
table-driven tests for failure at every step and assert that no later calls are
made.

### 3. High — Delayed validation ignores the pane's current authoritative agent hint

**Impact:** A pane that was Claude when scheduled but is now identified by the
runtime as Codex can receive Claude's `Escape`, text, and `Enter` sequence.
Stale Claude output plus a generic foreground wrapper process is sufficient to
pass the current checks.

**Evidence:** The registry documents a non-empty agent hint as authoritative
(`internal/provider/registry.go:5-6`, `21-46`), and the job records the original
agent (`internal/jobs/manager.go:180-201`). However, delayed validation only
checks that the pane ID still exists and never compares `candidate.Agent` with
the stored provider/agent (`internal/jobs/validate.go:37-62`). It resolves the
stored provider with an empty content argument
(`internal/jobs/manager.go:369-375`). The Herdr adapter reduces process identity
to the first foreground process's name and CWD, so shared wrappers such as
`node` do not close this gap (`internal/runtime/herdr/herdr.go:136-153`).

**Validation:** A deterministic probe scheduled a Claude job and later returned
the same pane ID with `Agent: "codex"`, matching `node`/CWD metadata, and mixed
stale Claude plus current Codex content. Validation proceeded and sent Claude's
Escape sequence.

**Recommendation:** resolve the current provider from
`candidate.Agent` and current content at send time. Require it to match the
stored job provider, and fail closed on unknown or conflicting non-empty hints.
Persist and compare stronger runtime identity if Herdr exposes it.

### 4. High — Any pane-content change can falsely verify a still-limited resume

**Impact:** A timestamp, footer, prompt echo, or other unrelated output change
marks the job `RESUMED` even when the rate-limit detector still says the pane is
limited. On a later poll, the changed hash allows `HandleLimit` to create a new
job for the same episode, so one limit can produce duplicate resume attempts.

**Evidence:** verification succeeds when either the limit disappears **or** the
SHA-256 of the entire pane content differs
(`internal/jobs/validate.go:85-106`;
`internal/jobs/manager.go:402-405`). The existing
`TestVerificationSucceedsWhenEvidenceHashChanges` appends `"new output"` to
still-limited content and expects `RESUMED`
(`internal/jobs/resume_test.go:95-106`). `HandleLimit` permits a new job when the
previous job is `RESUMED` and the raw evidence hash differs
(`internal/jobs/manager.go:169-175`).

**Recommendation:** verification should require affirmative provider-specific
evidence that the limit cleared or that the expected action completed. Keep an
episode fingerprint derived from normalized limit evidence, not the complete
screen, and do not re-arm solely because unrelated pane output changed.

### 5. Medium — Poll interval and verification timeout share one ticker but are validated independently

**Impact:** With `--interval >= --verify-timeout`, the first verification tick
arrives at or after the deadline. A successfully resumed pane is therefore
marked `FAILED` without any timely verification opportunity. Long monitoring
intervals make this configuration easy to create.

**Evidence:** the CLI checks that both durations are positive but not their
relationship (`runcmd.go:57-117`). `manager.Tick` is a post-poll hook on the
single `cfg.Interval` ticker (`runcmd.go:225-267`), while verification treats
`now >= VerifyDeadlineUTC` as failure (`internal/jobs/validate.go:97-115`).

**Recommendation:** schedule due-job advancement/verification on a separate,
short bounded ticker. As a minimum fail-fast guard, reject configurations where
the interval is not comfortably below the verification timeout.

### 6. Medium — The non-persistent coordinator path reports success after send failure

**Impact:** When a resume action fails outside the persistent job path,
`ContinueSent` is still set, future attempts are suppressed, an action is
recorded, the run loop logs a continuation, and a success notification is sent.
Operators see success for an action that may not have occurred.

**Evidence:** `sendResume` explicitly discards the error and records the action
(`internal/coordinator/coordinator.go:287-295`); its callers then set
`ContinueSent` or periodic timestamps (`internal/coordinator/coordinator.go:185-203`).
`RunLoop` treats that action record as success and notifies
(`internal/coordinator/loop.go:39-52`).

**Recommendation:** return the error from `sendResume`; only mutate success
state, log, and notify after the complete sequence succeeds. Log/notify failure
separately and choose a bounded retry policy.

### 7. Medium — `doctor` claims protocol compatibility without inspecting the response

**Impact:** `doctor` can report `PASS status: protocol 17` for any command output
that exits successfully, including a response with an incompatible or missing
protocol. This gives false confidence precisely where the command is intended
to diagnose runtime compatibility.

**Evidence:** `protocolDetail` ignores its input and always returns the same
known-good string (`doctorcmd.go:89-91`). `runDoctorCommand` reports PASS after
`status` or `api snapshot` exits zero, without decoding a protocol value
(`doctorcmd.go:137-146`).

**Recommendation:** decode and validate the actual protocol/version field. If
the available command cannot expose it, report only command reachability and
label protocol compatibility `WARN` or `UNKNOWN`, not `PASS`.

### 8. Medium — Long polling can miss a transient limit without creating a durable job

**Impact:** A limit banner can appear and become stale between action-capable
polls. No job is persisted, so reaching the reset time or waiting for later
polls cannot resume the session. This is a silent failure of the primary
auto-resume path, and increasing `--verify-timeout` does not mitigate it.

**Evidence:** startup discovers panes with `coord.Poll()` while their modes are
still off, then calls `coord.EnableAll()` (`runcmd.go:246-258`). `EnableAll`
updates rate-limit display state but does not invoke the job sink
(`internal/coordinator/coordinator.go:231-239`, `260-285`). The next
action-capable poll occurs only when the configured ticker fires
(`internal/coordinator/loop.go:23-37`). If non-chrome output appears below the
banner before that poll, the deliberate stale-output guard refuses action
(`internal/detection/live.go:29-47`, `202-212`). A ten-minute interval therefore
creates both an initial blind window and recurring windows long enough to miss
short-lived evidence.

**Live validation:** On 2026-07-31 the deployed watcher was alive with
`--interval 10m --verify-timeout 30m` and included Claude pane `wA:p1`. After
the reported reset there was no state file or scheduled job. The pane contained
the earlier `resets 10am (UTC)` banner followed by a newer idle prompt, and the
deployed detector returned `IsLimited=false`. A fail-stop manual submission of
`continue` was accepted and Herdr changed the pane to `agent_status=working`.
The exact missed transition was not instrumented, but the live evidence and
startup/polling path agree on the blind-window mechanism.

**Recommendation:** preserve the stale-output safety guard. Perform an
action-capable poll immediately after enabling panes, then decouple limit
acquisition from the long status/scheduler cadence by using a short bounded
detection interval or Herdr events. Add a regression covering a banner that is
visible during startup and gone before the first configured ticker event.

### 9. Low — The CLI accepts a zero safety margin but the job manager silently changes it

**Impact:** `--margin 0` is documented and accepted as non-negative, but jobs
are scheduled one minute later than requested. Status metadata therefore does
not reflect the operator's explicit configuration.

**Evidence:** flag validation rejects only negative values
(`runcmd.go:99-109`), while `withDefaults` replaces any margin less than or
equal to zero with one minute (`internal/jobs/manager.go:127-143`).

**Validation:** A focused probe passed `Config{Margin: 0}` and observed a
one-minute effective margin.

**Recommendation:** distinguish “not configured” from an explicit zero, for
example by applying the default in CLI parsing and letting the manager preserve
zero.

### 10. Low — Impossible calendar dates are normalized instead of rejected

**Impact:** malformed evidence such as `Feb 31` is accepted and silently moved
into March, potentially scheduling an automatic action for a date the provider
never supplied.

**Evidence:** date parsing validates only `1 <= day <= 31`
(`internal/detection/resetspec.go:261-274`) and passes the values directly to
`time.Date`, which normalizes impossible dates
(`internal/detection/resetspec.go:86-117`, `298-306`). Existing malformed-input
tests cover invalid clocks and excessive durations but not invalid
month/day combinations (`internal/detection/resetspec_test.go`).

**Validation:** A focused probe parsed `Feb 31, 2026` and observed a non-zero
March timestamp.

**Recommendation:** after constructing the date, compare its year, month, and
day with the parsed components and return an unknown reset on mismatch. Add
leap-year and short-month cases.

## Test and maintenance gaps

- There are no tests under `internal/tui`; layout and keyboard behavior depend
  on indirect/manual validation.
- The current gate has strong unit and race coverage but does not exercise
  multiple processes updating the state file. The cancellation probe should
  become a permanent regression/integration test when locking is implemented.
- Input-sequence tests verify the happy path but do not assert fail-stop
  behavior for each partial failure.
- The watcher cadence and verification cadence need a configuration-level test
  because fake-clock unit tests currently bypass the production ticker
  relationship.
- Startup and transient-limit acquisition need an end-to-end fake-clock test;
  current tests do not cover evidence disappearing before the first
  action-capable poll.

## Suggested remediation order

1. Make state transitions transactional and cancellation authoritative.
2. Make input sequences fail-stop, then propagate errors through the
   non-persistent path.
3. Revalidate current provider identity immediately before sending.
4. Tighten verification semantics and episode identity.
5. Eliminate the startup/transient detection blind window and decouple limit
   acquisition and scheduler verification from long status polling.
6. Correct `doctor`, zero-margin handling, and invalid-date validation.
