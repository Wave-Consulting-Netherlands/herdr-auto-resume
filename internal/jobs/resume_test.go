package jobs

import (
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

type saveSpy struct {
	path   string
	file   store.File
	saves  int
	failAt int
	onSave func(store.File)
}

func (s *saveSpy) Load() (store.File, error) { return s.file, nil }
func (s *saveSpy) Path() string              { return s.path }
func (s *saveSpy) Save(file store.File) error {
	s.saves++
	if s.onSave != nil {
		s.onSave(file)
	}
	if s.failAt != 0 && s.saves >= s.failAt {
		return errors.New("injected save failure")
	}
	s.file = file
	return nil
}

func newResumeManager(t *testing.T, rt *testRuntime, cfg Config, st store.Store) *Manager {
	t.Helper()
	return New(rt, st, cfg,
		WithClock(func() time.Time { return testNow }),
		WithSleep(func(time.Duration) {}),
		WithIDGenerator(func() string { return "job-1" }),
		WithLogWriter(io.Discard),
	)
}

func TestResumePersistsResumingBeforeSending(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
	spy := &saveSpy{path: filepath.Join(t.TempDir(), "state.json")}
	spy.onSave = func(file store.File) {
		if len(file.Jobs) == 1 && file.Jobs[0].State == store.StateResuming && len(rt.SentText) != 0 {
			t.Fatal("send occurred before RESUMING save")
		}
	}
	m := newResumeManager(t, rt, Config{Margin: time.Minute}, spy)
	m.HandleLimit(limitEvent(content, testNow))
	m.Tick(testNow.Add(time.Minute))
	if got := m.Snapshot()[0].State; got != store.StateVerifyingResume {
		t.Fatalf("state = %s, want VERIFYING_RESUME", got)
	}
	if len(rt.SentText) != 1 {
		t.Fatalf("sent text = %d, want 1", len(rt.SentText))
	}
}

func TestFailingResumingSaveSendsNothing(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
	spy := &saveSpy{path: filepath.Join(t.TempDir(), "state.json"), failAt: 4}
	m := newResumeManager(t, rt, Config{Margin: time.Minute}, spy)
	m.HandleLimit(limitEvent(content, testNow))
	m.Tick(testNow.Add(time.Minute))
	if len(rt.SentText) != 0 || len(rt.SentKeys) != 0 {
		t.Fatalf("runtime writes = text %#v keys %#v, want none", rt.SentText, rt.SentKeys)
	}
	if got := m.Snapshot()[0].State; got != store.StateValidating {
		t.Fatalf("state = %s, want reverted VALIDATING", got)
	}
}

func TestResumeSendsExactlyOnceAcrossManyTicks(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
	st := store.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	m := newResumeManager(t, rt, Config{Margin: time.Minute}, st)
	m.HandleLimit(limitEvent(content, testNow))
	for i := 0; i < 50; i++ {
		m.Tick(testNow.Add(time.Minute + time.Duration(i)*time.Second))
	}
	if len(rt.SentText) != 1 || len(rt.SentKeys) != 2 {
		t.Fatalf("send counts = text %d keys %d, want 1 and 2", len(rt.SentText), len(rt.SentKeys))
	}
}

func TestVerificationSucceedsWhenEvidenceHashChanges(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute, VerifyTimeout: time.Minute}, "job-1")
	m.HandleLimit(limitEvent(content, testNow))
	m.Tick(testNow.Add(time.Minute))
	rt.Content["p1"] = content + "\nnew output"
	m.Tick(testNow.Add(30 * time.Second))
	if got := m.Snapshot()[0].State; got != store.StateResumed {
		t.Fatalf("state = %s, want RESUMED", got)
	}
}

func TestVerificationSucceedsWhenRateLimitClears(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute, VerifyTimeout: time.Minute}, "job-1")
	m.HandleLimit(limitEvent(content, testNow))
	m.Tick(testNow.Add(time.Minute))
	rt.Content["p1"] = readyContent()
	m.Tick(testNow.Add(30 * time.Second))
	if got := m.Snapshot()[0].State; got != store.StateResumed {
		t.Fatalf("state = %s, want RESUMED", got)
	}
}

func TestVerificationDeadlineFailsAtExactDeadline(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute, VerifyTimeout: time.Minute}, "job-1")
	m.HandleLimit(limitEvent(content, testNow))
	m.Tick(testNow.Add(time.Minute))
	m.Tick(testNow.Add(2 * time.Minute))
	job := m.Snapshot()[0]
	if job.State != store.StateFailed || job.LastError == "" {
		t.Fatalf("job = %#v, want FAILED at exact deadline", job)
	}
}

func TestDryRunSkipsRuntimeWritesAndVerification(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute, DryRun: true}, "job-1")
	m.HandleLimit(limitEvent(content, testNow))
	m.Tick(testNow.Add(time.Minute))
	if len(rt.SentText) != 0 || len(rt.SentKeys) != 0 {
		t.Fatalf("runtime writes = text %#v keys %#v, want none", rt.SentText, rt.SentKeys)
	}
	if got := m.Snapshot()[0].State; got != store.StateResumed {
		t.Fatalf("state = %s, want RESUMED", got)
	}
}

func TestSendErrorRequiresManualIntervention(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}, Errs: map[string]error{"SendText": errors.New("send failed")}}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	m.HandleLimit(limitEvent(content, testNow))
	m.Tick(testNow.Add(time.Minute))
	job := m.Snapshot()[0]
	if job.State != store.StateManualRequired || job.LastError == "" {
		t.Fatalf("job = %#v, want MANUAL_REQUIRED with error", job)
	}
}
