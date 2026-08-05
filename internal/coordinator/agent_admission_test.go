package coordinator

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/codex"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

func agentAdmissionCoordinator(t *testing.T, enabled bool, log *bytes.Buffer) *Coordinator {
	t.Helper()
	return New(&runtime.Fake{}, Config{AdmitAgentEvents: enabled, OwnPaneID: "self:p1"},
		WithProviders(providerRegistryForAgentAdmission()), WithLogWriter(log))
}

func providerRegistryForAgentAdmission() *provider.Registry {
	return provider.NewRegistry(claude.New("continue"), codex.New("resume"))
}

func TestAgentDetectedAdmissionDefaultOffChangesNothing(t *testing.T) {
	var log bytes.Buffer
	c := agentAdmissionCoordinator(t, false, &log)
	var admitted []runtime.Pane
	c.AdmitAgentEventPanes([]runtime.Pane{{ID: "new:p1", Agent: "claude"}}, map[string]string{"new:p1": "claude"}, true,
		func(runtime.Pane) bool { return false }, "self:p1",
		func(pane runtime.Pane) { admitted = append(admitted, pane) }, time.Now())
	if len(admitted) != 0 || strings.Contains(log.String(), "agent-event admission") {
		t.Fatalf("admitted=%#v log=%q, want no admission", admitted, log.String())
	}
}

func TestAgentDetectedAdmissionAdmitsOnlyCurrentResolvableNonSelfPane(t *testing.T) {
	var log bytes.Buffer
	c := agentAdmissionCoordinator(t, true, &log)
	monitored := map[string]bool{"configured:p1": true}
	var admitted []runtime.Pane
	snapshot := []runtime.Pane{
		{ID: "configured:p1", Agent: "claude"},
		{ID: "new:p1", Agent: "claude"},
		{ID: "other:p1", Agent: "claude"},
		{ID: "shell:p1"},
		{ID: "unknown:p1", Agent: "gemini"},
		{ID: "self:p1", Agent: "claude"},
	}
	c.AdmitAgentEventPanes(snapshot, map[string]string{"new:p1": "claude"}, true, func(pane runtime.Pane) bool { return monitored[pane.ID] }, "self:p1",
		func(pane runtime.Pane) { monitored[pane.ID] = true; admitted = append(admitted, pane) }, time.Now())
	if len(admitted) != 1 || admitted[0].ID != "new:p1" {
		t.Fatalf("admitted=%#v, want only new:p1", admitted)
	}
	if got := log.String(); !strings.Contains(got, "agent-event admission: admitted pane=new:p1 agent=claude trigger=pane.agent_detected") {
		t.Fatalf("log=%q, want pane/agent/trigger admission line", got)
	}
}

func TestAgentDetectedAdmissionReplayAndReconnectAreIdempotentAndRequireFreshSnapshot(t *testing.T) {
	var log bytes.Buffer
	c := agentAdmissionCoordinator(t, true, &log)
	monitored := map[string]bool{}
	var admitted []runtime.Pane
	admit := func(pane runtime.Pane) { monitored[pane.ID] = true; admitted = append(admitted, pane) }
	check := func(eventPanes map[string]string, snapshot []runtime.Pane) {
		c.AdmitAgentEventPanes(snapshot, eventPanes, true, func(pane runtime.Pane) bool { return monitored[pane.ID] }, "self:p1", admit, time.Now())
	}
	// A replayed lifecycle frame is only a trigger; the reconciled snapshot is empty.
	check(map[string]string{"historical:p1": "claude"}, nil)
	if len(admitted) != 0 {
		t.Fatalf("historical replay admitted=%#v, want none", admitted)
	}
	check(map[string]string{"live:p1": "claude"}, []runtime.Pane{{ID: "live:p1", Agent: "claude"}})
	check(map[string]string{"live:p1": "claude"}, []runtime.Pane{{ID: "live:p1", Agent: "claude"}})
	if len(admitted) != 1 || strings.Count(log.String(), "pane=live:p1") != 1 {
		t.Fatalf("admitted=%#v log=%q, want one live admission", admitted, log.String())
	}
}

func TestAgentDetectedAdmissionNeverResurrectsExplicitlyDisabledPane(t *testing.T) {
	var log bytes.Buffer
	c := agentAdmissionCoordinator(t, true, &log)
	c.SetPanes([]runtime.Pane{{ID: "disabled:p1", Agent: "claude"}})
	c.Poll()
	c.EnableAll()
	c.ToggleMode("disabled:p1")
	var admitted bool
	c.AdmitAgentEventPanes([]runtime.Pane{{ID: "disabled:p1", Agent: "claude"}}, map[string]string{"disabled:p1": "claude"}, true,
		func(runtime.Pane) bool { return false }, "self:p1",
		func(runtime.Pane) { admitted = true }, time.Now())
	if admitted {
		t.Fatal("explicitly disabled pane was admitted")
	}
}

func TestAgentSnapshotAdmissionDefaultOffChangesNothing(t *testing.T) {
	var log bytes.Buffer
	c := agentAdmissionCoordinator(t, false, &log)
	var admitted []runtime.Pane
	c.AdmitAgentSnapshotPanes([]runtime.Pane{{ID: "new:p1", Agent: "claude"}}, true,
		func(runtime.Pane) bool { return false }, "self:p1",
		func(pane runtime.Pane) { admitted = append(admitted, pane) }, time.Now())
	if len(admitted) != 0 || strings.Contains(log.String(), "agent-event admission") {
		t.Fatalf("admitted=%#v log=%q, want no startup admission", admitted, log.String())
	}
}

func TestAgentSnapshotAdmissionAdmitsResolvableNonSelfPanesWithStartupTrigger(t *testing.T) {
	var log bytes.Buffer
	c := agentAdmissionCoordinator(t, true, &log)
	monitored := map[string]bool{"configured:p1": true}
	var admitted []runtime.Pane
	snapshot := []runtime.Pane{
		{ID: "configured:p1", Agent: "claude"},
		{ID: "new:p1", Agent: "claude"},
		{ID: "codex:p1", Agent: "codex"},
		{ID: "shell:p1"},
		{ID: "unknown:p1", Agent: "gemini"},
		{ID: "self:p1", Agent: "claude"},
	}
	c.AdmitAgentSnapshotPanes(snapshot, true, func(pane runtime.Pane) bool { return monitored[pane.ID] }, "self:p1",
		func(pane runtime.Pane) { monitored[pane.ID] = true; admitted = append(admitted, pane) }, time.Now())
	if len(admitted) != 2 || admitted[0].ID != "new:p1" || admitted[1].ID != "codex:p1" {
		t.Fatalf("admitted=%#v, want new:p1 and codex:p1", admitted)
	}
	if got := log.String(); strings.Count(got, "trigger=startup-snapshot") != 2 || strings.Contains(got, "trigger=pane.agent_detected") {
		t.Fatalf("log=%q, want two startup-snapshot admissions only", got)
	}
}

func TestAgentSnapshotAdmissionSecondPassIsIdempotent(t *testing.T) {
	var log bytes.Buffer
	c := agentAdmissionCoordinator(t, true, &log)
	monitored := map[string]bool{}
	var admitted []runtime.Pane
	admit := func(pane runtime.Pane) { monitored[pane.ID] = true; admitted = append(admitted, pane) }
	snapshot := []runtime.Pane{{ID: "new:p1", Agent: "claude"}}
	seed := func(panes []runtime.Pane) {
		c.AdmitAgentSnapshotPanes(panes, true, func(pane runtime.Pane) bool { return monitored[pane.ID] }, "self:p1", admit, time.Now())
	}
	seed(snapshot)
	seed(snapshot)
	if len(admitted) != 1 || strings.Count(log.String(), "pane=new:p1") != 1 {
		t.Fatalf("admitted=%#v log=%q, want one admission and one log", admitted, log.String())
	}
}

func TestAgentSnapshotAdmissionIncompleteSnapshotSeedsNothing(t *testing.T) {
	var log bytes.Buffer
	c := agentAdmissionCoordinator(t, true, &log)
	var admitted []runtime.Pane
	c.AdmitAgentSnapshotPanes([]runtime.Pane{{ID: "new:p1", Agent: "claude"}}, false,
		func(runtime.Pane) bool { return false }, "self:p1",
		func(pane runtime.Pane) { admitted = append(admitted, pane) }, time.Now())
	if len(admitted) != 0 || log.Len() != 0 {
		t.Fatalf("admitted=%#v log=%q, want no admission from incomplete snapshot", admitted, log.String())
	}
}

func TestAgentSnapshotAdmissionNeverResurrectsExplicitlyDisabledPane(t *testing.T) {
	var log bytes.Buffer
	c := agentAdmissionCoordinator(t, true, &log)
	c.SetPanes([]runtime.Pane{{ID: "disabled:p1", Agent: "claude"}})
	c.Poll()
	c.EnableAll()
	c.ToggleMode("disabled:p1")
	var admitted bool
	c.AdmitAgentSnapshotPanes([]runtime.Pane{{ID: "disabled:p1", Agent: "claude"}}, true,
		func(runtime.Pane) bool { return false }, "self:p1",
		func(runtime.Pane) { admitted = true }, time.Now())
	if admitted || log.Len() != 0 {
		t.Fatalf("admitted=%v log=%q, want disabled pane to remain silent and unadmitted", admitted, log.String())
	}
}

func TestAgentSnapshotAdmissionResyncSeedsPaneThatAppearedWhileStreamWasDown(t *testing.T) {
	var log bytes.Buffer
	c := agentAdmissionCoordinator(t, true, &log)
	monitored := map[string]bool{}
	var admitted []runtime.Pane
	admit := func(pane runtime.Pane) { monitored[pane.ID] = true; admitted = append(admitted, pane) }
	seed := func(panes []runtime.Pane) {
		c.AdmitAgentSnapshotPanes(panes, true, func(pane runtime.Pane) bool { return monitored[pane.ID] }, "self:p1", admit, time.Now())
	}
	seed([]runtime.Pane{{ID: "existing:p1", Agent: "claude"}})
	seed([]runtime.Pane{{ID: "existing:p1", Agent: "claude"}, {ID: "during-outage:p1", Agent: "claude"}})
	if len(admitted) != 2 || admitted[1].ID != "during-outage:p1" || strings.Count(log.String(), "trigger=startup-snapshot") != 2 {
		t.Fatalf("admitted=%#v log=%q, want resync admission for new pane", admitted, log.String())
	}
}
