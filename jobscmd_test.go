package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jobmanager "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/jobs"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func writeCommandState(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/state.json"
	st := store.NewJSONStore(path)
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{
		{ID: "abcdefgh-1", PaneID: "w1:p1", TerminalID: "term-1", Episode: "claude:session:123", Source: "session-file", State: store.StateWaiting, ResetAtUTC: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC), ResumeAtUTC: time.Date(2026, 7, 31, 12, 1, 0, 0, time.UTC), Attempts: 0},
		{ID: "ijklmnop-2", PaneID: "w1:p2", State: store.StateResumed, ResetAtUTC: time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC), ResumeAtUTC: time.Date(2026, 7, 31, 11, 1, 0, 0, time.UTC), Attempts: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStatusCommandGoldenShape(t *testing.T) {
	path := writeCommandState(t)
	var out, errOut bytes.Buffer
	if got := jobCommand([]string{"status", "--state-file", path}, &out, &errOut); got != 0 {
		t.Fatalf("jobCommand status exit = %d, stderr=%q", got, errOut.String())
	}
	wantHeader := "JOB       PANE   STATE    RESET(local)"
	if !strings.Contains(out.String(), wantHeader) || !strings.Contains(out.String(), "abcdefgh") || !strings.Contains(out.String(), "w1:p1") {
		t.Fatalf("status output = %q", out.String())
	}
}

func TestCodexBillingParkStatusInspectAckAndDedup(t *testing.T) {
	const (
		parkReason = "Codex billing action required: add workspace credits"
		content    = "workspace credits evidence v1"
	)
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	rt := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "codex"}}}
	ids := []string{"credits-1", "credits-2"}
	nextID := func() string {
		id := ids[0]
		ids = ids[1:]
		return id
	}
	m := jobmanager.New(rt, st, jobmanager.Config{}, jobmanager.WithIDGenerator(nextID))
	event := jobmanager.LimitEvent{
		Pane:       runtime.Pane{ID: "p1", Agent: "codex"},
		Provider:   "codex",
		Evidence:   content,
		ParkReason: parkReason,
		ObservedAt: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
	}
	if !m.HandleLimit(event) {
		t.Fatal("credits park was not owned")
	}
	if jobs := m.Snapshot(); len(jobs) != 1 || jobs[0].State != store.StateManualRequired || jobs[0].LastError != parkReason || !jobs[0].ResetAtUTC.IsZero() || !jobs[0].ResumeAtUTC.IsZero() {
		t.Fatalf("credits park jobs = %#v, want terminal billing park", jobs)
	}

	var statusOut, statusErr bytes.Buffer
	if got := jobCommand([]string{"status", "--state-file", path}, &statusOut, &statusErr); got != 0 || !strings.Contains(statusOut.String(), parkReason) {
		t.Fatalf("status exit=%d stdout=%q stderr=%q, want billing park reason", got, statusOut.String(), statusErr.String())
	}
	var inspectOut, inspectErr bytes.Buffer
	if got := jobCommand([]string{"inspect", "credits-1", "--state-file", path}, &inspectOut, &inspectErr); got != 0 || !strings.Contains(inspectOut.String(), `"park_reason": "`+parkReason+`"`) {
		t.Fatalf("inspect exit=%d stdout=%q stderr=%q, want billing park reason", got, inspectOut.String(), inspectErr.String())
	}

	var ackOut, ackErr bytes.Buffer
	if got := jobCommand([]string{"ack", "credits-1", "--state-file", path}, &ackOut, &ackErr); got != 0 {
		t.Fatalf("ack exit=%d stdout=%q stderr=%q", got, ackOut.String(), ackErr.String())
	}
	restarted := jobmanager.New(rt, st, jobmanager.Config{}, jobmanager.WithIDGenerator(nextID))
	if !restarted.HandleLimit(event) || len(restarted.Snapshot()) != 1 {
		t.Fatalf("identical billing evidence was not suppressed after ack: %#v", restarted.Snapshot())
	}
	changed := event
	changed.Evidence = "workspace credits evidence v2"
	if !restarted.HandleLimit(changed) || len(restarted.Snapshot()) != 2 || restarted.Snapshot()[1].ID != "credits-2" {
		t.Fatalf("changed billing evidence did not create a new job: %#v", restarted.Snapshot())
	}
}

func TestWriteJobStatusUsesProvidedLocationForReset(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	writeJobStatus(&out, []store.Job{{
		ID: "job-1", PaneID: "w1:p1", State: store.StateWaiting,
		ResetAtUTC:  time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		ResumeAtUTC: time.Date(2026, 1, 15, 12, 1, 0, 0, time.UTC),
	}}, loc)
	if !strings.Contains(out.String(), "2026-01-15T13:00:00+01:00") {
		t.Fatalf("status = %q, want Europe/Amsterdam reset rendering", out.String())
	}
}

func TestInspectUniquePrefixAndAmbiguity(t *testing.T) {
	path := writeCommandState(t)
	var out, errOut bytes.Buffer
	if got := jobCommand([]string{"inspect", "abcdefgh", "--state-file", path}, &out, &errOut); got != 0 {
		t.Fatalf("inspect exit = %d, stderr=%q", got, errOut.String())
	}
	var decoded store.Job
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("inspect JSON error = %v; output=%q", err, out.String())
	}
	if decoded.ID != "abcdefgh-1" {
		t.Fatalf("inspected job = %#v", decoded)
	}
	if decoded.TerminalID != "term-1" || !strings.Contains(out.String(), `"terminal_id": "term-1"`) {
		t.Fatalf("inspect output = %q, decoded = %#v", out.String(), decoded)
	}
	if decoded.Episode != "claude:session:123" || decoded.Source != "session-file" || !strings.Contains(out.String(), `"source": "session-file"`) {
		t.Fatalf("inspect episode/source = %#v, output = %q", decoded, out.String())
	}
	ambiguous := t.TempDir() + "/ambiguous.json"
	if err := store.NewJSONStore(ambiguous).Save(store.File{Version: 1, Jobs: []store.Job{{ID: "same-a"}, {ID: "same-b"}}}); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if got := jobCommand([]string{"inspect", "same", "--state-file", ambiguous}, &out, &errOut); got == 0 || !strings.Contains(errOut.String(), "ambiguous") {
		t.Fatalf("ambiguous inspect exit=%d stderr=%q", got, errOut.String())
	}
}

func TestCancelRoundTripAndTerminalRejection(t *testing.T) {
	path := writeCommandState(t)
	var out, errOut bytes.Buffer
	if got := jobCommand([]string{"cancel", "abcdefgh", "--state-file", path}, &out, &errOut); got != 0 {
		t.Fatalf("cancel exit = %d, stderr=%q", got, errOut.String())
	}
	file, err := store.NewJSONStore(path).Load()
	if err != nil || file.Jobs[0].State != store.StateCancelled {
		t.Fatalf("cancelled file = %#v, err=%v", file, err)
	}
	errOut.Reset()
	if got := jobCommand([]string{"cancel", "ijklmnop", "--state-file", path}, &out, &errOut); got == 0 || !strings.Contains(errOut.String(), "terminal") {
		t.Fatalf("terminal cancel exit=%d stderr=%q", got, errOut.String())
	}
}

func TestCLIJobDispatch(t *testing.T) {
	path := writeCommandState(t)
	var out, errOut bytes.Buffer
	if got := runCLI([]string{"status", "--state-file", path}, &out, &errOut); got != 0 {
		t.Fatalf("runCLI status exit = %d, stderr=%q", got, errOut.String())
	}
}

func TestJobCommandsSurfaceFutureStateVersion(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tc := range []struct {
		name string
		verb string
		args []string
	}{
		{name: "status", verb: "status"},
		{name: "inspect", verb: "inspect", args: []string{"job"}},
		{name: "cancel", verb: "cancel", args: []string{"job"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, []byte(`{"version":2,"jobs":[]}`), 0600); err != nil {
				t.Fatal(err)
			}
			args := append([]string{tc.verb}, tc.args...)
			args = append(args, "--state-file", path)
			var out, errOut bytes.Buffer
			if got := jobCommand(args, &out, &errOut); got != 1 || !strings.Contains(errOut.String(), "error: load state:") || !strings.Contains(errOut.String(), "version 2") || !strings.Contains(errOut.String(), "supported version 1") {
				t.Fatalf("exit=%d stdout=%q stderr=%q; want surfaced future-version error", got, out.String(), errOut.String())
			}
		})
	}
}

func TestJobCommandUsesStateFileFromConfigWhenFlagUnset(t *testing.T) {
	statePath := writeCommandState(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nstate:\n  file: "+statePath+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if got := jobCommand([]string{"status", "--config", configPath}, &out, &errOut); got != 0 {
		t.Fatalf("jobCommand status exit = %d, stderr=%q", got, errOut.String())
	}
	if !strings.Contains(out.String(), "abcdefgh") {
		t.Fatalf("status output = %q, want configured state file", out.String())
	}
}

func TestAckTransitionMatrix(t *testing.T) {
	for _, tc := range []struct {
		name       string
		state      store.JobState
		wantExit   int
		wantState  store.JobState
		wantReason string
	}{
		{name: "manual required", state: store.StateManualRequired, wantExit: 0, wantState: store.StateManualRequired, wantReason: "resumed by hand"},
		{name: "session gone", state: store.StateSessionGone, wantExit: 0, wantState: store.StateSessionGone, wantReason: "acknowledged by operator"},
		{name: "failed", state: store.StateFailed, wantExit: 0, wantState: store.StateFailed, wantReason: "acknowledged by operator"},
		{name: "cancelled", state: store.StateCancelled, wantExit: 0, wantState: store.StateCancelled, wantReason: "acknowledged by operator"},
		{name: "disabled", state: store.StateDisabled, wantExit: 0, wantState: store.StateDisabled, wantReason: "acknowledged by operator"},
		{name: "resumed", state: store.StateResumed, wantExit: 1, wantState: store.StateResumed},
		{name: "waiting", state: store.StateWaiting, wantExit: 1, wantState: store.StateWaiting},
		{name: "validating", state: store.StateValidating, wantExit: 1, wantState: store.StateValidating},
		{name: "resuming", state: store.StateResuming, wantExit: 1, wantState: store.StateResuming},
		{name: "verifying", state: store.StateVerifyingResume, wantExit: 1, wantState: store.StateVerifyingResume},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := store.NewJSONStore(path).Save(store.File{Version: 1, Jobs: []store.Job{{ID: "job-ack", State: tc.state}}}); err != nil {
				t.Fatal(err)
			}
			args := []string{"ack", "job-ack", "--state-file", path}
			if tc.name == "manual required" {
				args = append(args, "--reason", "  resumed by hand  ")
			}
			var out, errOut bytes.Buffer
			if got := jobCommand(args, &out, &errOut); got != tc.wantExit {
				t.Fatalf("exit=%d stdout=%q stderr=%q, want %d", got, out.String(), errOut.String(), tc.wantExit)
			}
			file, err := store.NewJSONStore(path).Load()
			if err != nil {
				t.Fatal(err)
			}
			job := file.Jobs[0]
			if job.State != tc.wantState {
				t.Fatalf("state=%s, want %s", job.State, tc.wantState)
			}
			if tc.wantExit == 0 {
				if job.AckedAt.IsZero() || job.AckedReason != tc.wantReason {
					t.Fatalf("ack metadata=%#v, want timestamp and reason %q", job, tc.wantReason)
				}
			} else if !job.AckedAt.IsZero() || job.AckedReason != "" {
				t.Fatalf("rejected ack mutated metadata: %#v", job)
			}
		})
	}
}

func TestAckRejectsInvalidReason(t *testing.T) {
	for _, reason := range []string{"", "   ", strings.Repeat("x", 257)} {
		t.Run(fmt.Sprintf("reason-%d", len(reason)), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := store.NewJSONStore(path).Save(store.File{Version: 1, Jobs: []store.Job{{ID: "job-ack", State: store.StateFailed}}}); err != nil {
				t.Fatal(err)
			}
			var out, errOut bytes.Buffer
			if got := jobCommand([]string{"ack", "job-ack", "--reason", reason, "--state-file", path}, &out, &errOut); got != 2 || !strings.Contains(errOut.String(), "reason") {
				t.Fatalf("exit=%d stdout=%q stderr=%q, want reason validation error", got, out.String(), errOut.String())
			}
		})
	}
}

func TestAckAlreadyAckedIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	ackedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	if err := store.NewJSONStore(path).Save(store.File{Version: 1, Jobs: []store.Job{{ID: "job-ack", State: store.StateFailed, AckedAt: ackedAt, AckedReason: "original"}}}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if got := jobCommand([]string{"ack", "job-ack", "--reason", "replacement", "--state-file", path}, &out, &errOut); got != 0 || !strings.Contains(out.String(), "already acknowledged") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	file, err := store.NewJSONStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if !file.Jobs[0].AckedAt.Equal(ackedAt) || file.Jobs[0].AckedReason != "original" {
		t.Fatalf("idempotent ack changed metadata: %#v", file.Jobs[0])
	}
}

func TestAckPrefixResolution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := store.NewJSONStore(path).Save(store.File{Version: 1, Jobs: []store.Job{
		{ID: "abcdef01", State: store.StateFailed},
		{ID: "abcdef02", State: store.StateFailed},
	}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		id   string
		want string
	}{
		{name: "ambiguous", id: "abcdef", want: "ambiguous"},
		{name: "unknown", id: "missing", want: "no job matches"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if got := jobCommand([]string{"ack", tc.id, "--state-file", path}, &out, &errOut); got != 1 || !strings.Contains(errOut.String(), tc.want) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
			}
		})
	}
}

func TestStatusAndInspectShowParkedReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := store.NewJSONStore(path).Save(store.File{Version: 1, Jobs: []store.Job{{
		ID: "parked-job", PaneID: "w1:p1", State: store.StateManualRequired, LastValidation: "session ended before verification",
	}}}); err != nil {
		t.Fatal(err)
	}
	var statusOut, errOut bytes.Buffer
	if got := jobCommand([]string{"status", "--state-file", path}, &statusOut, &errOut); got != 0 || !strings.Contains(statusOut.String(), "PARKED") || !strings.Contains(statusOut.String(), "session ended before verification") {
		t.Fatalf("status exit=%d output=%q stderr=%q", got, statusOut.String(), errOut.String())
	}
	var inspectOut bytes.Buffer
	if got := jobCommand([]string{"inspect", "parked", "--state-file", path}, &inspectOut, &errOut); got != 0 {
		t.Fatalf("inspect exit=%d stderr=%q", got, errOut.String())
	}
	var inspected map[string]any
	if err := json.Unmarshal(inspectOut.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected["parked"] != true || inspected["park_reason"] != "session ended before verification" {
		t.Fatalf("inspect parked fields=%#v", inspected)
	}
}

type transitionStore struct {
	state      store.File
	staleRead  bool
	locked     bool
	transition bool
	saveCount  int
}

func (s *transitionStore) Load() (store.File, error) {
	if s.locked {
		return s.state, nil
	}
	if !s.staleRead {
		s.staleRead = true
		return s.state, nil
	}
	return s.state, nil
}

func (s *transitionStore) Save(file store.File) error {
	s.saveCount++
	s.state = file
	return nil
}

func (s *transitionStore) Path() string { return "" }

func (s *transitionStore) WithLock(fn func() error) error {
	if !s.transition {
		s.state.Jobs[0].State = store.StateResumed
		s.transition = true
	}
	s.locked = true
	defer func() { s.locked = false }()
	return fn()
}

func TestAckRechecksStateInsideLockAfterWatcherTransition(t *testing.T) {
	st := &transitionStore{state: store.File{Version: 1, Jobs: []store.Job{{ID: "job-ack", State: store.StateFailed}}}}
	var out, errOut bytes.Buffer
	if got := ackJob("job-ack", "acknowledged by operator", st, &out, &errOut); got != 1 || !strings.Contains(errOut.String(), "RESUMED") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	if st.saveCount != 0 || st.state.Jobs[0].State != store.StateResumed || !st.state.Jobs[0].AckedAt.IsZero() {
		t.Fatalf("ack clobbered concurrent transition: saves=%d state=%#v", st.saveCount, st.state.Jobs[0])
	}
}

func TestAckUnparksPaneAfterWatcherReloadWithChangedEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{{
		ID: "old-job", PaneID: "w1:p1", State: store.StateManualRequired,
		EvidenceHash: "old-evidence", LastValidation: "resumed manually",
	}}}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if got := jobCommand([]string{"ack", "old-job", "--reason", "menu answered manually", "--state-file", path}, &out, &errOut); got != 0 {
		t.Fatalf("ack exit=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}

	reloaded := jobmanager.New(&runtime.Fake{}, st, jobmanager.Config{Provider: "claude"},
		jobmanager.WithIDGenerator(func() string { return "new-job" }),
		jobmanager.WithClock(func() time.Time { return time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC) }))
	if !reloaded.HandleLimit(jobmanager.LimitEvent{
		Pane:       runtime.Pane{ID: "w1:p1", Agent: "claude"},
		ResetTime:  time.Date(2026, 8, 5, 13, 0, 0, 0, time.UTC),
		Evidence:   "new-evidence",
		ObservedAt: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
	}) {
		t.Fatal("changed evidence was not scheduled after watcher reload")
	}
	file, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Jobs) != 2 || file.Jobs[1].ID != "new-job" || file.Jobs[1].State != store.StateWaiting {
		t.Fatalf("reloaded state=%#v, want old acked job plus new waiting job", file.Jobs)
	}
}

func TestJSONStoreRoundTripsAckMetadataAndLoadsV02Shape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := []byte(`{"version":1,"jobs":[{"id":"old-job","state":"MANUAL_REQUIRED","acked_at":"2026-08-05T12:00:00Z","acked_reason":"resumed by hand"}]}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := store.NewJSONStore(path).Load()
	if err != nil || len(file.Jobs) != 1 || file.Jobs[0].AckedReason != "resumed by hand" || file.Jobs[0].AckedAt.IsZero() {
		t.Fatalf("loaded ack metadata=%#v err=%v", file, err)
	}
	if err := store.NewJSONStore(path).Save(file); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := store.NewJSONStore(path).Load()
	if err != nil || !roundTrip.Jobs[0].AckedAt.Equal(file.Jobs[0].AckedAt) || roundTrip.Jobs[0].AckedReason != "resumed by hand" {
		t.Fatalf("round-trip ack metadata=%#v err=%v", roundTrip, err)
	}

	v02Path := filepath.Join(t.TempDir(), "v02-state.json")
	if err := os.WriteFile(v02Path, []byte(`{"version":1,"jobs":[{"id":"v02-job","state":"WAITING"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if loaded, err := store.NewJSONStore(v02Path).Load(); err != nil || len(loaded.Jobs) != 1 || loaded.Jobs[0].AckedReason != "" || !loaded.Jobs[0].AckedAt.IsZero() {
		t.Fatalf("v0.2-shaped state load=%#v err=%v", loaded, err)
	}
}
