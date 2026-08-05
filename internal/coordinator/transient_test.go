package coordinator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

type transientRecordingSink struct {
	events []TransientEvent
	limits []LimitEvent
	owned  bool
}

func (s *transientRecordingSink) HandleLimit(event LimitEvent) bool {
	s.limits = append(s.limits, event)
	return s.owned
}
func (s *transientRecordingSink) HandleTransient(event TransientEvent) bool {
	s.events = append(s.events, event)
	return s.owned
}

func TestTransientRetryDefaultOffCreatesNoJobOrLog(t *testing.T) {
	content := "API error: connection reset"
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}}, Content: map[string]string{"p1": content}}
	sink := &transientRecordingSink{owned: true}
	var log bytes.Buffer
	c := New(fake, Config{}, WithJobSink(sink), WithLogWriter(&log))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	c.Poll()
	if len(sink.events) != 0 || len(fake.SentText) != 0 || len(fake.SentKeys) != 0 || log.Len() != 0 {
		t.Fatalf("events=%d sends=%#v/%#v log=%q; want all empty while default-off", len(sink.events), fake.SentText, fake.SentKeys, log.String())
	}
}

func TestTransientRetryEnabledClassifiesWithoutLimitEvent(t *testing.T) {
	content := "API error: connection reset"
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}}, Content: map[string]string{"p1": content}}
	sink := &transientRecordingSink{owned: true}
	var log bytes.Buffer
	c := New(fake, Config{TransientRetry: true}, WithJobSink(sink), WithLogWriter(&log))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	c.Poll()
	if len(sink.events) != 1 || sink.events[0].TransientClass == "" {
		t.Fatalf("transient events=%#v, want one classified event", sink.events)
	}
	if sink.events[0].Provider != "claude" || strings.Contains(log.String(), "limit diagnostic") || !strings.Contains(log.String(), "transient diagnostic pane=p1 provider=claude reason=classified") {
		t.Fatalf("event=%#v log=%q, want Claude transient and no limit diagnostic", sink.events[0], log.String())
	}
}

func TestResetBearingLimitWinsOverTransientInCoordinator(t *testing.T) {
	content := "You've hit your limit · resets 2pm\nAPI error: connection reset"
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}}, Content: map[string]string{"p1": content}}
	sink := &transientRecordingSink{owned: true}
	c := New(fake, Config{TransientRetry: true}, WithJobSink(sink))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	c.Poll()
	if len(sink.events) != 0 || len(sink.limits) != 1 {
		t.Fatalf("transient events=%#v limit events=%#v, want one limit and no transient", sink.events, sink.limits)
	}
}

func TestTransientWithoutAgentHintIsNeverResolvedOrAdmitted(t *testing.T) {
	content := "api error: connection reset"
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[enabled], func(t *testing.T) {
			fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}}
			sink := &transientRecordingSink{owned: true}
			c := New(fake, Config{TransientRetry: enabled, AdmitAgentEvents: true}, WithJobSink(sink))
			c.SetPanes(fake.PanesList)
			c.Poll()
			c.EnableAll()
			c.Poll()
			if state := c.Snapshot()[0]; state.Provider != "" || state.HasClaudeCode || len(sink.events) != 0 || len(sink.limits) != 0 {
				t.Fatalf("state=%#v events=%#v limits=%#v, want no identity or jobs", state, sink.events, sink.limits)
			}
			admitted := false
			c.AdmitAgentEventPanes(fake.PanesList, map[string]string{"p1": ""}, true, func(runtime.Pane) bool { return false }, "", func(runtime.Pane) { admitted = true }, coordinatorTestNow)
			if admitted {
				t.Fatal("transient-only pane was admitted without an agent identity")
			}
		})
	}
}
