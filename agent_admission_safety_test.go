package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/coordinator"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/jobs"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/codex"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func TestEventAdmittedPaneStillUsesEveryForeignPaneResumeGate(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		mutate func(*runtime.Fake)
	}{
		{name: "process changed", mutate: func(rt *runtime.Fake) { rt.Procs["new:p1"] = runtime.ProcessInfo{Command: "bash", CWD: "/work"} }},
		{name: "cwd changed", mutate: func(rt *runtime.Fake) { rt.Procs["new:p1"] = runtime.ProcessInfo{Command: "claude", CWD: "/other"} }},
		{name: "provider mismatch", mutate: func(rt *runtime.Fake) { rt.PanesList[0].Agent = "codex" }},
		{name: "terminal id changed", mutate: func(rt *runtime.Fake) { rt.PanesList[0].TerminalID = "term-2" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pane := runtime.Pane{ID: "new:p1", TerminalID: "term-1", Agent: "claude", CWD: "/work"}
			rt := &runtime.Fake{
				PanesList: []runtime.Pane{pane},
				Content:   map[string]string{"new:p1": "You've hit your limit · resets 5m"},
				Procs:     map[string]runtime.ProcessInfo{"new:p1": {Command: "claude", CWD: "/work"}},
			}
			registry := provider.NewRegistry(claude.New("continue"), codex.New("resume"))
			manager := jobs.New(rt, store.NewJSONStore(filepath.Join(t.TempDir(), "state.json")), jobs.Config{Margin: 0, VerifyTimeout: time.Minute},
				jobs.WithClock(func() time.Time { return now }), jobs.WithSleep(func(time.Duration) {}), jobs.WithIDGenerator(func() string { return "job-1" }), jobs.WithProviders(registry))
			coord := coordinator.New(rt, coordinator.Config{AdmitAgentEvents: true, OwnPaneID: "self:p1", ReadLines: 200},
				coordinator.WithClock(func() time.Time { return now }), coordinator.WithProviders(registry), coordinator.WithJobSink(manager))
			monitored := map[string]bool{}
			coord.AdmitAgentEventPanes(rt.PanesList, map[string]string{"new:p1": "claude"}, true, func(pane runtime.Pane) bool { return monitored[pane.ID] }, "self:p1", func(pane runtime.Pane) { monitored[pane.ID] = true }, now)
			coord.SetPanes(rt.PanesList)
			coord.Poll()
			coord.EnableAll()
			coord.Poll()
			if got := len(manager.Snapshot()); got != 1 {
				t.Fatalf("jobs = %d, want one job from event-admitted pane", got)
			}
			tc.mutate(rt)
			manager.Tick(now.Add(6 * time.Minute))
			job := manager.Snapshot()[0]
			if job.State != store.StateManualRequired {
				t.Fatalf("job = %#v, want MANUAL_REQUIRED for %s", job, tc.name)
			}
			if len(rt.SentText) != 0 || len(rt.SentKeys) != 0 {
				t.Fatalf("runtime writes = text %#v keys %#v, want no injection", rt.SentText, rt.SentKeys)
			}
		})
	}
}

func TestSeededPaneStillUsesEveryForeignPaneResumeGate(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		mutate func(*runtime.Fake)
	}{
		{name: "process changed", mutate: func(rt *runtime.Fake) { rt.Procs["seeded:p1"] = runtime.ProcessInfo{Command: "bash", CWD: "/work"} }},
		{name: "cwd changed", mutate: func(rt *runtime.Fake) { rt.Procs["seeded:p1"] = runtime.ProcessInfo{Command: "claude", CWD: "/other"} }},
		{name: "provider mismatch", mutate: func(rt *runtime.Fake) { rt.PanesList[0].Agent = "codex" }},
		{name: "terminal id changed", mutate: func(rt *runtime.Fake) { rt.PanesList[0].TerminalID = "term-2" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pane := runtime.Pane{ID: "seeded:p1", TerminalID: "term-1", Agent: "claude", CWD: "/work"}
			rt := &runtime.Fake{
				PanesList: []runtime.Pane{pane},
				Content:   map[string]string{"seeded:p1": "You've hit your limit · resets 5m"},
				Procs:     map[string]runtime.ProcessInfo{"seeded:p1": {Command: "claude", CWD: "/work"}},
			}
			registry := provider.NewRegistry(claude.New("continue"), codex.New("resume"))
			manager := jobs.New(rt, store.NewJSONStore(filepath.Join(t.TempDir(), "state.json")), jobs.Config{Margin: 0, VerifyTimeout: time.Minute},
				jobs.WithClock(func() time.Time { return now }), jobs.WithSleep(func(time.Duration) {}), jobs.WithIDGenerator(func() string { return "job-1" }), jobs.WithProviders(registry))
			coord := coordinator.New(rt, coordinator.Config{AdmitAgentEvents: true, OwnPaneID: "self:p1", ReadLines: 200},
				coordinator.WithClock(func() time.Time { return now }), coordinator.WithProviders(registry), coordinator.WithJobSink(manager))
			monitored := map[string]bool{}
			coord.AdmitAgentSnapshotPanes(rt.PanesList, true, func(pane runtime.Pane) bool { return monitored[pane.ID] }, "self:p1", func(pane runtime.Pane) { monitored[pane.ID] = true }, now)
			coord.SetPanes(rt.PanesList)
			coord.Poll()
			coord.EnableAll()
			coord.Poll()
			if got := len(manager.Snapshot()); got != 1 {
				t.Fatalf("jobs = %d, want one job from seeded pane", got)
			}
			tc.mutate(rt)
			manager.Tick(now.Add(6 * time.Minute))
			job := manager.Snapshot()[0]
			if job.State != store.StateManualRequired {
				t.Fatalf("job = %#v, want MANUAL_REQUIRED for %s", job, tc.name)
			}
			if len(rt.SentText) != 0 || len(rt.SentKeys) != 0 {
				t.Fatalf("runtime writes = text %#v keys %#v, want no injection", rt.SentText, rt.SentKeys)
			}
		})
	}
}
