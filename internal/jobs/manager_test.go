package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

var testNow = time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

type testRuntime struct {
	runtime.Fake
	listErr error
}

func (r *testRuntime) ListPanes() ([]runtime.Pane, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.Fake.ListPanes()
}

type testNotification struct {
	title string
	body  string
}

type testIDGenerator struct {
	ids []string
	i   int
}

func (g *testIDGenerator) next() string {
	if g.i >= len(g.ids) {
		return "generated"
	}
	id := g.ids[g.i]
	g.i++
	return id
}

func newTestManager(t *testing.T, rt runtime.Runtime, cfg Config, ids ...string) (*Manager, store.Store) {
	t.Helper()
	st := store.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	gen := &testIDGenerator{ids: ids}
	m := New(rt, st, cfg, WithClock(func() time.Time { return testNow }), WithSleep(func(time.Duration) {}), WithIDGenerator(gen.next), WithLogWriter(io.Discard))
	return m, st
}

func limitEvent(content string, reset time.Time) LimitEvent {
	return LimitEvent{Pane: runtime.Pane{ID: "p1", Agent: "claude"}, ResetsRaw: "5m", ResetTime: reset, Content: content, ObservedAt: testNow}
}

func limitedContent() string { return "You've hit your limit · resets 5m" }

func readyContent() string { return "╭────╮\n> " }

func TestHandleLimitRequiresNonZeroResetTime(t *testing.T) {
	rt := &testRuntime{}
	m, _ := newTestManager(t, rt, Config{}, "id")
	if m.HandleLimit(limitEvent("limit reached", time.Time{})) {
		t.Fatal("HandleLimit() = true for zero reset time")
	}
	if len(m.Snapshot()) != 0 {
		t.Fatal("zero-reset event created a job")
	}
}

func TestHandleLimitCreatesOneJobForRepeatedPolls(t *testing.T) {
	content := limitedContent()
	reset := testNow.Add(5 * time.Minute)
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
	m, _ := newTestManager(t, rt, Config{Provider: "claude", Margin: time.Minute}, "job-1")
	for i := 0; i < 100; i++ {
		if !m.HandleLimit(limitEvent(content, reset)) {
			t.Fatalf("HandleLimit() = false on call %d", i)
		}
	}
	jobs := m.Snapshot()
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs))
	}
	job := jobs[0]
	if job.ID != "job-1" || job.State != store.StateWaiting || job.ProcCommand != "claude" || job.WorkingDir != "/work" {
		t.Fatalf("job = %#v", job)
	}
	if !job.ResetAtUTC.Equal(reset) || !job.ResumeAtUTC.Equal(reset.Add(time.Minute)) || job.MarginSecs != 60 {
		t.Fatalf("schedule = reset %v resume %v margin %d", job.ResetAtUTC, job.ResumeAtUTC, job.MarginSecs)
	}
	wantHash := sha256.Sum256([]byte(content))
	if job.EvidenceHash != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("evidence hash = %q", job.EvidenceHash)
	}
}

func TestHandleLimitBeyondHorizonCreatesFailedOwnedJob(t *testing.T) {
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}}}
	m, _ := newTestManager(t, rt, Config{MaxHorizon: time.Hour}, "job-1")
	if !m.HandleLimit(limitEvent(limitedContent(), testNow.Add(2*time.Hour))) {
		t.Fatal("HandleLimit() = false for horizon failure")
	}
	jobs := m.Snapshot()
	if len(jobs) != 1 || jobs[0].State != store.StateFailed || !strings.Contains(jobs[0].LastError, "horizon") {
		t.Fatalf("jobs = %#v", jobs)
	}
	if len(rt.Notes) != 1 || !strings.Contains(rt.Notes[0].Body, "horizon") {
		t.Fatalf("notifications = %#v", rt.Notes)
	}
}

func TestTickWaitsThenValidatesOnResumeTime(t *testing.T) {
	content := limitedContent()
	reset := testNow.Add(time.Hour)
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	m.HandleLimit(limitEvent(content, reset))
	m.Tick(reset.Add(30 * time.Second))
	if got := m.Snapshot()[0].State; got != store.StateWaiting {
		t.Fatalf("early state = %s, want WAITING", got)
	}
	m.Tick(reset.Add(time.Minute))
	job := m.Snapshot()[0]
	if job.State != store.StateVerifyingResume || job.LastValidation == "" {
		t.Fatalf("at-resume job = %#v, want VERIFYING_RESUME with validation record", job)
	}
}

func TestValidationFingerprintAndCWDMismatchRequireManual(t *testing.T) {
	cases := []struct {
		name string
		proc runtime.ProcessInfo
	}{
		{name: "command", proc: runtime.ProcessInfo{Command: "other", CWD: "/work"}},
		{name: "cwd", proc: runtime.ProcessInfo{Command: "claude", CWD: "/other"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := limitedContent()
			rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
			m, _ := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
			m.HandleLimit(limitEvent(content, testNow))
			rt.Procs["p1"] = tc.proc
			m.Tick(testNow.Add(time.Minute))
			if got := m.Snapshot()[0].State; got != store.StateManualRequired {
				t.Fatalf("state = %s, want MANUAL_REQUIRED", got)
			}
		})
	}
}

func TestValidationPaneGoneAndTransientRuntimeOutage(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{PanesList: nil, Content: map[string]string{"p1": content}}, listErr: errors.New("offline")}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	m.HandleLimit(limitEvent(content, testNow))
	m.Tick(testNow.Add(time.Minute))
	if got := m.Snapshot()[0].State; got != store.StateValidating {
		t.Fatalf("outage state = %s, want VALIDATING", got)
	}
	rt.listErr = nil
	rt.PanesList = []runtime.Pane{{ID: "p1"}}
	rt.Procs = map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}
	m.Tick(testNow.Add(2 * time.Minute))
	if got := m.Snapshot()[0].State; got != store.StateVerifyingResume {
		t.Fatalf("recovered state = %s, want VERIFYING_RESUME", got)
	}
}

func TestValidationPaneGone(t *testing.T) {
	rt := &testRuntime{Fake: runtime.Fake{PanesList: nil, Content: map[string]string{"p1": limitedContent()}}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	m.HandleLimit(limitEvent(limitedContent(), testNow))
	m.Tick(testNow.Add(time.Minute))
	if got := m.Snapshot()[0].State; got != store.StateSessionGone {
		t.Fatalf("gone state = %s, want SESSION_GONE", got)
	}
}

func TestValidationRejectsMenuAndNonClaude(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "menu", content: "╭────╮\n❯ Upgrade\n> "},
		{name: "not Claude", content: "$ echo ready\nready"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": tc.content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
			m, _ := newTestManager(t, rt, Config{}, "job-1")
			m.HandleLimit(limitEvent(limitedContent(), testNow))
			m.Tick(testNow.Add(time.Minute))
			if got := m.Snapshot()[0].State; got != store.StateManualRequired {
				t.Fatalf("state = %s, want MANUAL_REQUIRED", got)
			}
		})
	}
}

func TestIdlePromptPassesValidation(t *testing.T) {
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": readyContent()}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
	m, _ := newTestManager(t, rt, Config{}, "job-1")
	m.HandleLimit(limitEvent(limitedContent(), testNow))
	m.Tick(testNow.Add(time.Minute))
	if got := m.Snapshot()[0].State; got != store.StateVerifyingResume {
		t.Fatalf("state = %s, want VERIFYING_RESUME", got)
	}
}

func TestProcessInfoErrorLeavesFingerprintEmptyAndRelaxesValidation(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Errs: map[string]error{"ProcessInfo": errors.New("unsupported")}}}
	m, st := newTestManager(t, rt, Config{}, "job-1")
	m.HandleLimit(limitEvent(content, testNow))
	if job := m.Snapshot()[0]; job.ProcCommand != "" || job.WorkingDir != "" {
		t.Fatalf("job fingerprint = %#v, want empty", job)
	}
	m.Tick(testNow.Add(time.Minute))
	if got := m.Snapshot()[0].State; got != store.StateVerifyingResume {
		t.Fatalf("state = %s, want VERIFYING_RESUME", got)
	}
	loaded, err := st.Load()
	if err != nil || len(loaded.Jobs) != 1 {
		t.Fatalf("stored jobs = %#v, err=%v", loaded, err)
	}
}

func TestDeterministicIDsAndStayArmedSecondJob(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}}}
	m, st := newTestManager(t, rt, Config{}, "first", "second")
	m.HandleLimit(limitEvent(content, testNow))
	file := m.Snapshot()
	file[0].State = store.StateResumed
	if err := st.Save(store.File{Version: 1, Jobs: file}); err != nil {
		t.Fatal(err)
	}
	m2 := New(rt, st, Config{}, WithClock(func() time.Time { return testNow }), WithSleep(func(time.Duration) {}), WithIDGenerator(func() string { return "second" }), WithLogWriter(io.Discard))
	if !m2.HandleLimit(limitEvent(content+"\nnew limit", testNow.Add(time.Hour))) {
		t.Fatal("second limit was not owned")
	}
	jobs := m2.Snapshot()
	if len(jobs) != 2 || jobs[0].ID != "first" || jobs[1].ID != "second" {
		t.Fatalf("jobs = %#v", jobs)
	}
}
