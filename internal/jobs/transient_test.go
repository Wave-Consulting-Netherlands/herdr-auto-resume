package jobs

import (
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func transientEvent(content string) TransientEvent {
	match, _ := detection.ClassifyTransient(content)
	return TransientEvent{
		Pane:     runtime.Pane{ID: "p1", Agent: "claude", TerminalID: "term-1", WorkspaceID: "ws-1"},
		Provider: "claude", TransientClass: match.Class, Content: content, Evidence: content, ObservedAt: testNow,
	}
}

func transientRuntime(content string) *testRuntime {
	return &testRuntime{Fake: runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: "claude", TerminalID: "term-1", WorkspaceID: "ws-1"}},
		Content:   map[string]string{"p1": content},
		Procs:     map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}},
	}}
}

func TestTransientBackoffSequenceCapsAndParks(t *testing.T) {
	content := "API error: connection reset"
	rt := transientRuntime(content)
	m, _ := newTestManager(t, rt, Config{TransientRetry: true, TransientMaxAttempts: 5, VerifyTimeout: 10 * time.Second}, "job-1")
	if !m.HandleTransient(transientEvent(content)) {
		t.Fatal("HandleTransient() = false")
	}

	wantDelays := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	now := testNow
	for attempt, wantDelay := range wantDelays {
		job := m.Snapshot()[0]
		if job.State != store.StateWaiting || job.RetryAttempt != attempt+1 {
			t.Fatalf("attempt %d job=%#v, want WAITING retry attempt %d", attempt+1, job, attempt+1)
		}
		if got := job.ResumeAtUTC.Sub(now); got != wantDelay {
			t.Fatalf("attempt %d delay=%s, want %s", attempt+1, got, wantDelay)
		}
		now = job.ResumeAtUTC
		m.Tick(now)
		if got := m.Snapshot()[0].State; got != store.StateVerifyingResume {
			t.Fatalf("attempt %d state after due tick=%s, want VERIFYING_RESUME", attempt+1, got)
		}
		now = now.Add(10 * time.Second)
		m.Tick(now)
		if attempt < len(wantDelays)-1 && m.Snapshot()[0].State != store.StateWaiting {
			t.Fatalf("attempt %d state after failed verification=%s, want WAITING", attempt+1, m.Snapshot()[0].State)
		}
	}
	job := m.Snapshot()[0]
	if job.State != store.StateManualRequired || !strings.Contains(job.LastError, "transient retry attempt limit") {
		t.Fatalf("terminal job=%#v, want clear attempt-cap terminal reason", job)
	}
}

func TestTransientSingleFlightSuppressesParallelJobs(t *testing.T) {
	content := "API error: connection reset"
	rt := transientRuntime(content)
	m, _ := newTestManager(t, rt, Config{TransientRetry: true}, "job-1", "job-2")
	if !m.HandleTransient(transientEvent(content)) || !m.HandleTransient(transientEvent(content)) {
		t.Fatal("repeated HandleTransient() should be owned by the existing flight")
	}
	if jobs := m.Snapshot(); len(jobs) != 1 || jobs[0].RetryAttempt != 1 {
		t.Fatalf("jobs=%#v, want one in-flight retry", jobs)
	}
}

func TestManagerTransientRetryDefaultOffCreatesNoJob(t *testing.T) {
	rt := transientRuntime("API error: connection reset")
	m, _ := newTestManager(t, rt, Config{}, "job-1")
	if m.HandleTransient(transientEvent("API error: connection reset")) {
		t.Fatal("HandleTransient() = true with transient retry disabled")
	}
	if jobs := m.Snapshot(); len(jobs) != 0 {
		t.Fatalf("jobs=%#v, want none while transient retry is disabled", jobs)
	}
}

func TestResetBearingLimitSupersedesPendingTransientJob(t *testing.T) {
	content := "API error: connection reset"
	rt := transientRuntime(content)
	m, _ := newTestManager(t, rt, Config{TransientRetry: true}, "transient-job", "limit-job")
	if !m.HandleTransient(transientEvent(content)) {
		t.Fatal("HandleTransient() = false")
	}
	if !m.HandleLimit(limitEvent(limitedContent(), testNow.Add(time.Hour))) {
		t.Fatal("HandleLimit() = false")
	}
	jobs := m.Snapshot()
	if len(jobs) != 2 || jobs[0].State != store.StateManualRequired || jobs[1].Source == "transient" || jobs[1].State != store.StateWaiting {
		t.Fatalf("jobs=%#v, want parked transient followed by waiting limit job", jobs)
	}
}

func TestTransientRetryUsesAllForeignPaneGates(t *testing.T) {
	content := "API error: connection reset"
	cases := []struct {
		name   string
		mutate func(*testRuntime)
	}{
		{name: "process", mutate: func(rt *testRuntime) { rt.Procs["p1"] = runtime.ProcessInfo{Command: "other", CWD: "/work"} }},
		{name: "cwd", mutate: func(rt *testRuntime) { rt.Procs["p1"] = runtime.ProcessInfo{Command: "claude", CWD: "/other"} }},
		{name: "provider", mutate: func(rt *testRuntime) { rt.PanesList[0].Agent = "codex" }},
		{name: "terminal", mutate: func(rt *testRuntime) { rt.PanesList[0].TerminalID = "term-2" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := transientRuntime(content)
			m, _ := newTestManager(t, rt, Config{TransientRetry: true, TransientMaxAttempts: 2}, "job-1")
			if !m.HandleTransient(transientEvent(content)) {
				t.Fatal("HandleTransient() = false")
			}
			tc.mutate(rt)
			m.Tick(testNow.Add(time.Minute))
			job := m.Snapshot()[0]
			if job.State != store.StateManualRequired || len(rt.SentText) != 0 || len(rt.SentKeys) != 0 {
				t.Fatalf("job=%#v sends=%#v/%#v, want parked with no injection", job, rt.SentText, rt.SentKeys)
			}
		})
	}
}

func TestTransientRetryRejectsMenuAndIdleScreens(t *testing.T) {
	content := "API error: connection reset"
	for _, tc := range []struct {
		name    string
		content string
	}{
		{name: "menu", content: content + "\nWhat do you want to do?\n❯ 1. Stop and wait for limit to reset\nEnter to confirm · Esc to cancel"},
		{name: "idle", content: readyContent()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := transientRuntime(content)
			m, _ := newTestManager(t, rt, Config{TransientRetry: true}, "job-1")
			if !m.HandleTransient(transientEvent(content)) {
				t.Fatal("HandleTransient() = false")
			}
			rt.Content["p1"] = tc.content
			m.Tick(testNow.Add(time.Minute))
			if job := m.Snapshot()[0]; job.State != store.StateManualRequired || len(rt.SentText) != 0 || len(rt.SentKeys) != 0 {
				t.Fatalf("job=%#v sends=%#v/%#v, want parked with no injection", job, rt.SentText, rt.SentKeys)
			}
		})
	}
}

func TestTransientRetryLogsAttemptAndDelay(t *testing.T) {
	content := "API error: connection reset"
	rt := transientRuntime(content)
	m, _ := newTestManager(t, rt, Config{TransientRetry: true, TransientMaxAttempts: 2, VerifyTimeout: time.Second}, "job-1")
	var log strings.Builder
	m.logw = &log
	if !m.HandleTransient(transientEvent(content)) {
		t.Fatal("HandleTransient() = false")
	}
	if !strings.Contains(log.String(), "transient retry pane=p1 attempt=1 next_delay=1m0s") {
		t.Fatalf("initial log=%q, want attempt 1 and 60s", log.String())
	}
	m.Tick(testNow.Add(time.Minute))
	m.Tick(testNow.Add(time.Minute + time.Second))
	if !strings.Contains(log.String(), "transient retry pane=p1 attempt=2 next_delay=2m0s") {
		t.Fatalf("second log=%q, want attempt 2 and 120s", log.String())
	}
}
