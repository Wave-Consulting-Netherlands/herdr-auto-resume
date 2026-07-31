package jobs

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/codex"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func jobsPhase5Registry() *provider.Registry {
	return provider.NewRegistry(claude.New(""), codex.New(""))
}

func codexLimitEvent(content string) LimitEvent {
	return LimitEvent{
		Pane:       runtime.Pane{ID: "p1", Agent: "codex"},
		Provider:   "codex",
		ResetsRaw:  "3:51 PM",
		ResetTime:  testNow.Add(time.Minute),
		Spec:       detection.ResetSpec{Kind: detection.ResetKindLocalClock, Raw: "3:51 PM", ParsedTime: testNow.Add(time.Minute), Confidence: detection.ConfidenceHigh},
		Content:    content,
		ObservedAt: testNow,
	}
}

func TestCodexJobCycleSendsPromptAndEnterWithoutEscape(t *testing.T) {
	content := "■ You've hit your usage limit. Try again at 3:51 PM.\n› Ask Codex to do anything"
	rt := &testRuntime{Fake: runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: "codex"}},
		Content:   map[string]string{"p1": content},
		Procs:     map[string]runtime.ProcessInfo{"p1": {Command: "codex", CWD: "/work"}},
	}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	m.providers = jobsPhase5Registry()
	if !m.HandleLimit(codexLimitEvent(content)) {
		t.Fatal("HandleLimit() = false")
	}
	m.Tick(testNow.Add(2 * time.Minute))
	if len(rt.SentText) != 1 || rt.SentText[0].Text != codex.New("").ResumeAction().Text {
		t.Fatalf("sent text = %#v, want exactly one Codex prompt", rt.SentText)
	}
	if len(rt.SentKeys) != 1 || len(rt.SentKeys[0].Keys) != 1 || rt.SentKeys[0].Keys[0] != "enter" {
		t.Fatalf("sent keys = %#v, want exactly one Enter and no Escape", rt.SentKeys)
	}
	if got := m.Snapshot()[0].Provider; got != "codex" {
		t.Fatalf("job provider = %q, want codex", got)
	}
	rt.Content["p1"] = "› Ask Codex to do anything"
	m.Tick(testNow.Add(3 * time.Minute))
	if got := m.Snapshot()[0].State; got != store.StateResumed {
		t.Fatalf("final state = %s, want RESUMED", got)
	}
}

func TestCodexJobShellReplacementRequiresManual(t *testing.T) {
	content := "■ You've hit your usage limit. Try again at 3:51 PM.\n› Ask Codex to do anything"
	rt := &testRuntime{Fake: runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: "codex"}},
		Content:   map[string]string{"p1": content},
		Procs:     map[string]runtime.ProcessInfo{"p1": {Command: "codex", CWD: "/work"}},
	}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	m.providers = jobsPhase5Registry()
	m.HandleLimit(codexLimitEvent(content))
	rt.Content["p1"] = "$ echo replaced\n$"
	m.Tick(testNow.Add(2 * time.Minute))
	if got := m.Snapshot()[0].State; got != store.StateManualRequired {
		t.Fatalf("state = %s, want MANUAL_REQUIRED", got)
	}
	if len(rt.SentText) != 0 || len(rt.SentKeys) != 0 {
		t.Fatalf("runtime writes = %#v/%#v, want none", rt.SentText, rt.SentKeys)
	}
}

func TestSchemaOneEmptyProviderUsesClaudeCompatibilityFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	content := limitedContent()
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{{ID: "old", PaneID: "p1", State: store.StateWaiting, ResetAtUTC: testNow, ResumeAtUTC: testNow}}}); err != nil {
		t.Fatal(err)
	}
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}}}}
	m := New(rt, st, Config{}, WithClock(func() time.Time { return testNow }), WithSleep(func(time.Duration) {}))
	m.Tick(testNow)
	if got := m.Snapshot()[0].State; got != store.StateVerifyingResume {
		t.Fatalf("state = %s, want VERIFYING_RESUME", got)
	}
}

func TestUnknownProviderStateRequiresManual(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{{ID: "unknown", Provider: "gemini", PaneID: "p1", State: store.StateWaiting, ResetAtUTC: testNow, ResumeAtUTC: testNow}}}); err != nil {
		t.Fatal(err)
	}
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": limitedContent()}}}
	m := New(rt, st, Config{}, WithClock(func() time.Time { return testNow }), WithSleep(func(time.Duration) {}))
	m.Tick(testNow)
	if got := m.Snapshot()[0].State; got != store.StateManualRequired {
		t.Fatalf("state = %s, want MANUAL_REQUIRED", got)
	}
	if len(rt.SentText) != 0 || len(rt.SentKeys) != 0 {
		t.Fatalf("runtime writes = %#v/%#v, want none", rt.SentText, rt.SentKeys)
	}
}
