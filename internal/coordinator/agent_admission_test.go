package coordinator

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

func agentAdmissionCoordinator(t *testing.T, enabled bool, log *bytes.Buffer) *Coordinator {
	t.Helper()
	return New(&runtime.Fake{}, Config{AdmitAgentEvents: enabled, OwnPaneID: "self:p1"},
		WithProviders(providerRegistryForAgentAdmission()), WithLogWriter(log))
}

func providerRegistryForAgentAdmission() *provider.Registry {
	return provider.NewRegistry(claude.New("continue"))
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
