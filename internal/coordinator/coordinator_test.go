package coordinator

import (
	"reflect"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

type recordingJobSink struct {
	events []LimitEvent
	owned  bool
}

func (s *recordingJobSink) HandleLimit(event LimitEvent) bool {
	s.events = append(s.events, event)
	return s.owned
}

func TestJobSinkReceivesKnownResetPayloadEveryPollAndOwnsLegacySend(t *testing.T) {
	content := "limit reached ∙ resets 2pm"
	status := detection.CheckRateLimit(content)
	now := status.ResetTime.Add(5 * time.Minute)
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Title: "Claude"}}, Content: map[string]string{"p1": content}}
	sink := &recordingJobSink{owned: true}
	c := New(fake, Config{ReadLines: 10}, WithClock(func() time.Time { return now }), WithJobSink(sink))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	c.Poll()
	c.Poll()

	if len(sink.events) != 2 {
		t.Fatalf("sink calls = %d, want 2", len(sink.events))
	}
	event := sink.events[0]
	if event.Pane != fake.PanesList[0] || event.ResetsRaw != "2pm" || event.ResetTime.IsZero() || event.Content != content || !event.ObservedAt.Equal(now) {
		t.Fatalf("event = %#v, want pane/reset/content/clock payload", event)
	}
	if len(fake.SentText) != 0 || len(fake.SentKeys) != 0 {
		t.Fatalf("legacy sends = text %#v keys %#v, want none", fake.SentText, fake.SentKeys)
	}
	if !c.Snapshot()[0].ContinueSent {
		t.Fatal("ContinueSent = false, want owned episode latched")
	}
}

func TestJobSinkFalseFailsSafeWithoutLegacySend(t *testing.T) {
	content := "limit reached ∙ resets 2pm"
	status := detection.CheckRateLimit(content)
	now := status.ResetTime.Add(5 * time.Minute)
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}}
	sink := &recordingJobSink{}
	c := New(fake, Config{}, WithClock(func() time.Time { return now }), WithJobSink(sink), WithSleep(func(time.Duration) {}))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	c.Poll()
	if len(sink.events) != 1 || len(fake.SentText) != 0 || len(fake.SentKeys) != 0 {
		t.Fatalf("sink events=%d sends=%#v/%#v, want one sink call and no sends", len(sink.events), fake.SentText, fake.SentKeys)
	}
}

func TestUnknownResetNeverCallsJobSink(t *testing.T) {
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": "limit reached"}}
	sink := &recordingJobSink{}
	c := New(fake, Config{}, WithJobSink(sink), WithClock(func() time.Time { return time.Unix(0, 0) }), WithSleep(func(time.Duration) {}))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	c.Poll()
	if len(sink.events) != 0 {
		t.Fatalf("sink calls = %d, want 0 for unknown reset", len(sink.events))
	}
}

func TestModeOffAndTestPatternNeverCallJobSink(t *testing.T) {
	content := "limit reached ∙ resets 2pm\n<<<TEST>>>"
	status := detection.CheckRateLimit(content)
	now := status.ResetTime.Add(5 * time.Minute)
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}}
	sink := &recordingJobSink{owned: true}
	c := New(fake, Config{TestPattern: "<<<TEST>>>"}, WithJobSink(sink), WithClock(func() time.Time { return now }), WithSleep(func(time.Duration) {}))
	c.SetPanes(fake.PanesList)
	c.Poll()
	if len(sink.events) != 0 {
		t.Fatal("sink called while mode is off")
	}
	c.ToggleMode("p1")
	c.Poll()
	if len(sink.events) != 1 {
		t.Fatalf("sink calls in auto mode = %d, want one", len(sink.events))
	}
}

func TestKnownResetSendsOnceAfterReset(t *testing.T) {
	content := "limit reached ∙ resets 2pm"
	status := detection.CheckRateLimit(content)
	now := status.ResetTime.Add(-5 * time.Minute)
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}}
	c := New(fake, Config{TestPattern: "", ReadLines: 10}, WithClock(func() time.Time { return now }), WithSleep(func(time.Duration) {}))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	c.Poll()
	if len(fake.SentText) != 0 {
		t.Fatal("known reset sent before reset time")
	}
	now = status.ResetTime.Add(5 * time.Minute)
	c.Poll()
	c.Poll()

	if len(fake.SentText) != 1 || len(fake.SentKeys) != 2 {
		t.Fatalf("send counts = text %d, keys %d, want 1 and 2", len(fake.SentText), len(fake.SentKeys))
	}
	if !c.Snapshot()[0].ContinueSent {
		t.Fatal("ContinueSent = false, want latched true")
	}
}

func TestUnknownResetSendsEveryFifteenMinutes(t *testing.T) {
	now := time.Now()
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": "limit reached"}}
	c := New(fake, Config{ReadLines: 10}, WithClock(func() time.Time { return now }), WithSleep(func(time.Duration) {}))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	c.Poll()
	now = now.Add(14*time.Minute + 59*time.Second)
	c.Poll()
	if len(fake.SentText) != 1 {
		t.Fatalf("send count at +14m59s = %d, want 1", len(fake.SentText))
	}
	now = now.Add(time.Second)
	c.Poll()
	if len(fake.SentText) != 2 {
		t.Fatalf("send count at +15m = %d, want 2", len(fake.SentText))
	}
}

func TestNewLimitTransitionResetsLatch(t *testing.T) {
	now := time.Now()
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": "limit reached"}}
	c := New(fake, Config{}, WithClock(func() time.Time { return now }), WithSleep(func(time.Duration) {}))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	c.Poll()
	fake.Content["p1"] = "┌────┐\n> ready"
	c.Poll()
	fake.Content["p1"] = "limit reached"
	c.Poll()
	if len(fake.SentText) != 2 {
		t.Fatalf("send count after new limit = %d, want 2", len(fake.SentText))
	}
}

func TestPollExcludesOwnPane(t *testing.T) {
	fake := &runtime.Fake{
		PanesList: []runtime.Pane{{ID: "self"}, {ID: "other"}},
		Self:      "self",
		Content:   map[string]string{"self": "limit reached", "other": "┌────┐\n> ready"},
	}
	c := New(fake, Config{OwnPaneID: "self"})
	c.SetPanes(fake.PanesList)
	c.Poll()
	states := c.Snapshot()
	if states[0].HasClaudeCode {
		t.Fatal("own pane HasClaudeCode = true, want false")
	}
	for _, call := range fake.Calls {
		if call == "ReadPane(self)" {
			t.Fatal("own pane was read")
		}
	}
}

func TestPollClearsNonClaudeRateState(t *testing.T) {
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": "limit reached"}}
	c := New(fake, Config{})
	c.SetPanes(fake.PanesList)
	c.Poll()
	state := c.states["p1"]
	state.Mode = ModeAuto
	c.states["p1"] = state
	c.Poll()
	if !c.states["p1"].IsRateLimited {
		t.Fatal("setup did not produce limited state")
	}
	fake.Content["p1"] = "plain shell output"
	c.Poll()
	state = c.Snapshot()[0]
	if state.IsRateLimited || state.RateLimitResets != "" || !state.RateLimitTime.IsZero() || state.ContinueSent || !state.LastPeriodicContinue.IsZero() {
		t.Fatalf("non-Claude state not cleared: %#v", state)
	}
}

func TestSetPanesPreservesStateAndPrunesVanishedPanes(t *testing.T) {
	first := runtime.Pane{ID: "p1", Title: "old", Left: 1}
	fake := &runtime.Fake{}
	c := New(fake, Config{})
	c.SetPanes([]runtime.Pane{first, {ID: "p2"}})
	c.states["p1"] = PaneState{
		Pane:                 first,
		Mode:                 ModeAuto,
		HasClaudeCode:        true,
		IsRateLimited:        true,
		RateLimitResets:      "2pm",
		RateLimitTime:        time.Unix(10, 0),
		ContinueSent:         true,
		LastPeriodicContinue: time.Unix(20, 0),
	}
	c.SetPanes([]runtime.Pane{{ID: "p1", Title: "new", Left: 9}})
	state := c.Snapshot()[0]
	if state.Pane.Title != "new" || state.Pane.Left != 9 {
		t.Fatalf("pane descriptor not refreshed: %#v", state.Pane)
	}
	if state.Mode != ModeAuto || !state.HasClaudeCode || !state.IsRateLimited || state.RateLimitResets != "2pm" || state.RateLimitTime != time.Unix(10, 0) || !state.ContinueSent || state.LastPeriodicContinue != time.Unix(20, 0) {
		t.Fatalf("state not preserved: %#v", state)
	}
	if _, ok := c.states["p2"]; ok {
		t.Fatal("vanished pane p2 was not pruned")
	}
}

func TestTestPatternFiresOnceOnlyInAutoMode(t *testing.T) {
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": "┌────┐\n> <<<TEST>>>"}}
	c := New(fake, Config{TestPattern: "<<<TEST>>>"}, WithSleep(func(time.Duration) {}))
	c.SetPanes(fake.PanesList)
	c.Poll()
	if len(fake.SentText) != 0 {
		t.Fatal("test pattern fired while mode was off")
	}
	c.ToggleMode("p1")
	c.Poll()
	c.Poll()
	if len(fake.SentText) != 1 || !c.Snapshot()[0].ContinueSent {
		t.Fatalf("pattern sends = %d, latch = %v, want one and true", len(fake.SentText), c.Snapshot()[0].ContinueSent)
	}
}

func TestSendContinueOrderAndSleep(t *testing.T) {
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": "┌────┐\n> <<<TEST>>>"}}
	var sleeps []time.Duration
	c := New(fake, Config{TestPattern: "<<<TEST>>>"}, WithSleep(func(d time.Duration) { sleeps = append(sleeps, d) }))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	fake.Calls = nil
	c.Poll()
	want := []string{"ReadPane(p1)", "SendKeys(p1,escape)", "SendText(p1,continue)", "SendKeys(p1,enter)"}
	if !reflect.DeepEqual(fake.Calls, want) {
		t.Fatalf("Calls = %#v, want %#v", fake.Calls, want)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{100 * time.Millisecond}) {
		t.Fatalf("sleeps = %#v", sleeps)
	}
}

func TestDryRunRecordsActionWithoutSending(t *testing.T) {
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": "┌────┐\n> <<<TEST>>>"}}
	c := New(fake, Config{TestPattern: "<<<TEST>>>", DryRun: true})
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	c.Poll()
	if len(fake.SentText) != 0 || len(fake.SentKeys) != 0 {
		t.Fatalf("dry run sent text=%#v keys=%#v", fake.SentText, fake.SentKeys)
	}
	action, ok := c.LastAction()
	if !ok || action.PaneID != "p1" || action.Kind != "continue" || !action.DryRun {
		t.Fatalf("LastAction = %#v, %v", action, ok)
	}
}

func TestToggleModeEnablesAndChecksRateLimit(t *testing.T) {
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": "limit reached"}}
	c := New(fake, Config{})
	c.SetPanes(fake.PanesList)
	state := c.states["p1"]
	state.HasClaudeCode = true
	state.ContinueSent = true
	c.states["p1"] = state
	fake.Calls = nil
	c.ToggleMode("p1")
	state = c.Snapshot()[0]
	if state.Mode != ModeAuto || !state.IsRateLimited || state.ContinueSent {
		t.Fatalf("state after enable = %#v", state)
	}
	if !reflect.DeepEqual(fake.Calls, []string{"ReadPane(p1)"}) {
		t.Fatalf("immediate check calls = %#v", fake.Calls)
	}
}
