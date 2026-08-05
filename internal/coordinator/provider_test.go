package coordinator

import (
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/codex"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

func phase5Registry() *provider.Registry {
	return provider.NewRegistry(claude.New(""), codex.New(""))
}

func TestCodexPollingStampsProviderAndNeverUsesPeriodicNudge(t *testing.T) {
	content := "■ You've hit your usage limit. Try again later.\n› Ask Codex to do anything"
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "codex"}}, Content: map[string]string{"p1": content}}
	sink := &recordingJobSink{owned: true}
	now := coordinatorTestNow
	c := New(fake, Config{}, WithProviders(phase5Registry()), WithClock(func() time.Time { return now }), WithJobSink(sink), WithSleep(func(time.Duration) {}))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.EnableAll()
	now = now.Add(30 * time.Minute)
	c.Poll()

	state := c.Snapshot()[0]
	if state.Provider != "codex" || state.HasClaudeCode || !state.IsRateLimited {
		t.Fatalf("state = %#v, want codex limited non-Claude state", state)
	}
	if len(sink.events) != 0 || len(fake.SentText) != 0 || len(fake.SentKeys) != 0 {
		t.Fatalf("events=%#v sends=%#v/%#v, want no event or periodic send", sink.events, fake.SentText, fake.SentKeys)
	}
}

func TestCodexBillingBannersCreateDistinctParkEventsWithoutAction(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		reason  string
	}{
		{
			name:    "workspace credits",
			content: "■ Your workspace is out of credits. Add credits to continue.\n› Ask Codex to do anything",
			reason:  "Codex billing action required: add workspace credits",
		},
		{
			name:    "spend cap",
			content: "■ You hit your spend cap set in your workspace. Increase your spend cap to continue.\n› Ask Codex to do anything",
			reason:  "Codex billing action required: raise the workspace spend cap",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "codex"}}, Content: map[string]string{"p1": tc.content}}
			sink := &recordingJobSink{owned: true}
			c := New(fake, Config{}, WithProviders(phase5Registry()), WithClock(func() time.Time { return coordinatorTestNow }), WithJobSink(sink), WithSleep(func(time.Duration) {}))
			c.SetPanes(fake.PanesList)
			c.Poll()
			c.EnableAll()
			c.Poll()

			if len(sink.events) != 1 {
				t.Fatalf("sink events = %d, want one billing park event", len(sink.events))
			}
			event := sink.events[0]
			if event.ResetTime.IsZero() == false || event.ParkReason != tc.reason {
				t.Fatalf("event = %#v, want zero reset and park reason %q", event, tc.reason)
			}
			if strings.Contains(strings.ToLower(event.ParkReason), "retry") || len(fake.SentText) != 0 || len(fake.SentKeys) != 0 {
				t.Fatalf("event/actions = %#v/%#v/%#v, want billing park with no retry or action", event, fake.SentText, fake.SentKeys)
			}
		})
	}
}

func TestAgentHintWinsOverContentDetection(t *testing.T) {
	content := "⎿ You've hit your limit · resets 3pm (UTC)\nOpening your options…\n❯"
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "codex"}}, Content: map[string]string{"p1": content}}
	sink := &recordingJobSink{owned: true}
	c := New(fake, Config{}, WithProviders(phase5Registry()), WithClock(func() time.Time { return coordinatorTestNow }), WithJobSink(sink), WithSleep(func(time.Duration) {}))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.EnableAll()
	c.Poll()
	if len(sink.events) != 0 || len(fake.SentText) != 0 || len(fake.SentKeys) != 0 {
		t.Fatalf("hint-wins events=%#v sends=%#v/%#v, want none", sink.events, fake.SentText, fake.SentKeys)
	}
}

func TestNoHintAmbiguityFailsClosed(t *testing.T) {
	content := "⎿ You've hit your limit · resets 3pm (UTC)\n■ You've hit your usage limit. Try again at 3:51 PM.\n› Ask Codex to do anything"
	fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": content}}
	sink := &recordingJobSink{owned: true}
	c := New(fake, Config{}, WithProviders(phase5Registry()), WithClock(func() time.Time { return coordinatorTestNow }), WithJobSink(sink), WithSleep(func(time.Duration) {}))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.EnableAll()
	c.Poll()
	state := c.Snapshot()[0]
	if state.Provider != "" || state.HasClaudeCode || len(sink.events) != 0 {
		t.Fatalf("state=%#v events=%#v, want no provider or event", state, sink.events)
	}
}
