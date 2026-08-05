package jobs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

type menuRuntime struct {
	testRuntime
	afterSend  string
	beforeSend func()
}

func (r *menuRuntime) SendKeys(paneID string, keys ...string) error {
	if r.beforeSend != nil {
		r.beforeSend()
	}
	err := r.Fake.SendKeys(paneID, keys...)
	if err == nil && r.afterSend != "" {
		r.Content[paneID] = r.afterSend
	}
	return err
}

func readMenuFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "detection", "testdata", path))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func menuValidationContent(fixture string) string {
	// The committed captures are menu-focused tails; retain their exact menu
	// text while supplying the live Claude limit banner used by validation.
	return "Claude Code\n⎿ You've hit your limit · resets 5m\n" + fixture
}

func menuOnlyViewport(t *testing.T) string {
	t.Helper()
	return readMenuFixture(t, "claude/positive/cc2026-08_menu-team-plan.txt")
}

func menuEvent(content string) LimitEvent {
	pane := runtime.Pane{ID: "p1", Agent: "claude", AgentSessionID: "session-1"}
	reset := testNow.Add(time.Hour)
	return LimitEvent{
		Pane: pane, Provider: "claude", EpisodeID: "episode-1", ResetsRaw: "5m", ResetTime: reset,
		ObservedAt: testNow, Content: content,
	}
}

func menuManager(t *testing.T, content string, answer, dryRun bool) (*Manager, *menuRuntime, store.Store) {
	t.Helper()
	rt := &menuRuntime{testRuntime: testRuntime{Fake: runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: "claude", AgentSessionID: "session-1"}},
		Content:   map[string]string{"p1": content},
		Procs:     map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}},
	}}, afterSend: readyContent()}
	m, st := newTestManager(t, rt, Config{AnswerLimitMenu: answer, DryRun: dryRun, Margin: time.Minute}, "job-1")
	return m, rt, st
}

func runMenuValidation(t *testing.T, content string, answer, dryRun bool) (*Manager, *menuRuntime, store.Store) {
	t.Helper()
	m, rt, st := menuManager(t, content, answer, dryRun)
	if !m.HandleLimit(menuEvent(content)) {
		t.Fatal("HandleLimit() did not own menu episode")
	}
	m.Tick(testNow.Add(2 * time.Hour))
	return m, rt, st
}

func TestAnswerLimitMenuSupportsBothCommittedVariants(t *testing.T) {
	for _, path := range []string{
		"claude/positive/cc2026-08_menu-team-plan.txt",
		"rate_limit_new_format.txt",
	} {
		t.Run(path, func(t *testing.T) {
			content := menuValidationContent(readMenuFixture(t, path))
			m, rt, st := menuManager(t, content, true, false)
			rt.beforeSend = func() {
				file, err := st.Load()
				if err != nil || len(file.Jobs) != 1 || file.Jobs[0].MenuAttempt == nil {
					t.Errorf("state before send = %#v, err=%v; want persisted menu attempt", file, err)
				}
			}
			if !m.HandleLimit(menuEvent(content)) {
				t.Fatal("HandleLimit() did not own menu episode")
			}
			m.Tick(testNow.Add(2 * time.Hour))
			if len(rt.SentKeys) != 1 || rt.SentKeys[0].PaneID != "p1" || len(rt.SentKeys[0].Keys) != 1 || rt.SentKeys[0].Keys[0] != "enter" {
				t.Fatalf("sent keys = %#v, want exactly one enter", rt.SentKeys)
			}
			job := m.Snapshot()[0]
			if job.State != store.StateManualRequired || job.MenuAttempt == nil || job.MenuAttempt.SessionID != "session-1" || job.MenuAttempt.EpisodeID != "episode-1" || job.MenuAttempt.PaneID != "p1" {
				t.Fatalf("job = %#v, want persisted menu attempt and manual-required outcome", job)
			}
			if !strings.Contains(job.LastValidation, "menu gone") {
				t.Fatalf("validation = %q, want menu gone outcome", job.LastValidation)
			}
		})
	}
}

func TestMenuOnlyViewportHasNoClaudeChrome(t *testing.T) {
	content := menuOnlyViewport(t)
	if detection.IsClaudeCode(content) {
		t.Fatal("menu-only regression fixture unexpectedly contains Claude chrome")
	}
	if !looksLikeLimitMenu(content) {
		t.Fatal("menu-only regression fixture is not recognized as a limit menu")
	}
}

func TestMenuOnlyViewportWithAnswerLimitMenuAnswersBeforeIdentityGate(t *testing.T) {
	content := menuOnlyViewport(t)
	m, rt, _ := menuManager(t, content, true, false)
	if !m.HandleLimit(menuEvent(content)) {
		t.Fatal("HandleLimit() did not own menu episode")
	}
	m.Tick(testNow.Add(2 * time.Hour))

	job := m.Snapshot()[0]
	if len(rt.SentKeys) != 1 || rt.SentKeys[0].Keys[0] != "enter" {
		t.Fatalf("sent keys = %#v, want one menu answer", rt.SentKeys)
	}
	if job.MenuAttempt == nil || job.State != store.StateManualRequired {
		t.Fatalf("job = %#v, want persisted menu answer and manual-required outcome", job)
	}
	if strings.Contains(job.LastValidation, "pane is not claude") {
		t.Fatalf("validation = %q, menu-only pane hit content identity gate", job.LastValidation)
	}
}

func TestMenuOnlyViewportWithoutAgentHintUsesStoredClaudeProvider(t *testing.T) {
	content := menuOnlyViewport(t)
	m, rt, _ := menuManager(t, content, true, false)
	if !m.HandleLimit(menuEvent(content)) {
		t.Fatal("HandleLimit() did not own menu episode")
	}
	rt.PanesList[0].Agent = ""
	m.Tick(testNow.Add(2 * time.Hour))

	job := m.Snapshot()[0]
	if len(rt.SentKeys) != 1 || rt.SentKeys[0].Keys[0] != "enter" {
		t.Fatalf("sent keys = %#v, want one menu answer", rt.SentKeys)
	}
	if job.MenuAttempt == nil || strings.Contains(job.LastValidation, "unknown current provider") {
		t.Fatalf("job = %#v, want stored Claude provider rescue to reach menu answer", job)
	}
}

func TestMenuOnlyViewportWithoutAgentHintAndAnswerLimitMenuOffStillParksUnknownProvider(t *testing.T) {
	content := menuOnlyViewport(t)
	m, rt, _ := menuManager(t, content, false, false)
	if !m.HandleLimit(menuEvent(content)) {
		t.Fatal("HandleLimit() did not own menu episode")
	}
	rt.PanesList[0].Agent = ""
	m.Tick(testNow.Add(2 * time.Hour))

	job := m.Snapshot()[0]
	if len(rt.SentKeys) != 0 || job.State != store.StateManualRequired || !strings.Contains(job.LastValidation, "unknown current provider") {
		t.Fatalf("keys=%#v job=%#v, want unchanged unknown-provider park", rt.SentKeys, job)
	}
}

func TestMenuOnlyViewportWithoutAgentHintDoesNotRescueStoredCodexProvider(t *testing.T) {
	content := menuOnlyViewport(t)
	m, rt, _ := menuManager(t, content, true, false)
	event := menuEvent(content)
	event.Provider = "codex"
	if !m.HandleLimit(event) {
		t.Fatal("HandleLimit() did not own menu episode")
	}
	rt.PanesList[0].Agent = ""
	m.Tick(testNow.Add(2 * time.Hour))

	job := m.Snapshot()[0]
	if len(rt.SentKeys) != 0 || job.State != store.StateManualRequired || !strings.Contains(job.LastValidation, "unknown current provider") {
		t.Fatalf("keys=%#v job=%#v, want no cross-provider rescue", rt.SentKeys, job)
	}
}

func TestUnidentifiableContentWithoutAgentHintDoesNotUseStoredProviderRescue(t *testing.T) {
	content := "$ echo ready\nready"
	rt := &menuRuntime{testRuntime: testRuntime{Fake: runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: ""}},
		Content:   map[string]string{"p1": content},
		Procs:     map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}},
	}}}
	m, _ := newTestManager(t, rt, Config{AnswerLimitMenu: true, Margin: time.Minute}, "job-1")
	if !m.HandleLimit(menuEvent("You've hit your limit · resets 5m")) {
		t.Fatal("HandleLimit() did not own menu episode")
	}
	m.Tick(testNow.Add(2 * time.Hour))

	job := m.Snapshot()[0]
	if job.State != store.StateManualRequired || !strings.Contains(job.LastValidation, "unknown current provider") {
		t.Fatalf("job = %#v, want non-menu unknown-provider park", job)
	}
}

func TestMenuOnlyViewportWithoutAnswerLimitMenuParksAndLogsOnce(t *testing.T) {
	content := menuOnlyViewport(t)
	m, _, _ := menuManager(t, content, false, false)
	var log strings.Builder
	m.logw = &log
	if !m.HandleLimit(menuEvent(content)) {
		t.Fatal("HandleLimit() did not own menu episode")
	}
	m.Tick(testNow.Add(2 * time.Hour))
	m.Tick(testNow.Add(2*time.Hour + time.Second))

	job := m.Snapshot()[0]
	if job.State != store.StateManualRequired || !strings.Contains(job.LastValidation, "pane is not claude") {
		t.Fatalf("job = %#v, want unchanged identity-gate park", job)
	}
	if got := strings.Count(log.String(), "limit diagnostic"); got != 1 {
		t.Fatalf("diagnostic count = %d (%q), want one line", got, log.String())
	}
	for _, want := range []string{"pane=p1", "job=job-1", "provider=claude", "reason=pane-is-not-claude"} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("diagnostic = %q, want %q", log.String(), want)
		}
	}
}

func TestMenuOnlyViewportForeignPaneStillParks(t *testing.T) {
	content := menuOnlyViewport(t)
	cases := []struct {
		name   string
		mutate func(*menuRuntime)
		want   string
	}{
		{name: "agent hint mismatch", mutate: func(rt *menuRuntime) { rt.PanesList[0].Agent = "codex" }, want: "conflicts with job provider"},
		{name: "foreground process changed", mutate: func(rt *menuRuntime) { rt.Procs["p1"] = runtime.ProcessInfo{Command: "bash", CWD: "/work"} }, want: "foreground process changed"},
		{name: "working directory changed", mutate: func(rt *menuRuntime) { rt.Procs["p1"] = runtime.ProcessInfo{Command: "claude", CWD: "/other"} }, want: "working directory changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, rt, _ := menuManager(t, content, true, false)
			if !m.HandleLimit(menuEvent(content)) {
				t.Fatal("HandleLimit() did not own menu episode")
			}
			tc.mutate(rt)
			m.Tick(testNow.Add(2 * time.Hour))
			job := m.Snapshot()[0]
			if len(rt.SentKeys) != 0 || job.State != store.StateManualRequired || !strings.Contains(job.LastValidation, tc.want) {
				t.Fatalf("keys=%#v job=%#v, want parked foreign pane (%q)", rt.SentKeys, job, tc.want)
			}
		})
	}

	t.Run("reused pane id", func(t *testing.T) {
		m, rt, st := menuManager(t, content, true, false)
		event := menuEvent(content)
		event.Pane.TerminalID = "term-1"
		if !m.HandleLimit(event) {
			t.Fatal("HandleLimit() did not own menu episode")
		}
		file := m.Snapshot()
		if file[0].TerminalID != "term-1" {
			t.Fatalf("job terminal id = %q, want term-1", file[0].TerminalID)
		}
		rt.PanesList[0].TerminalID = "term-2"
		m.Tick(testNow.Add(2 * time.Hour))
		job := m.Snapshot()[0]
		if len(rt.SentKeys) != 0 || job.State != store.StateManualRequired || job.LastValidation != "pane identity changed" {
			t.Fatalf("keys=%#v job=%#v, want reused pane parked", rt.SentKeys, job)
		}
		loaded, err := st.Load()
		if err != nil || len(loaded.Jobs) != 1 {
			t.Fatalf("stored state = %#v, err=%v", loaded, err)
		}
	})
}

func TestMenuOnlyViewportCannotAnswerSameJobTwiceAcrossValidationPasses(t *testing.T) {
	content := menuOnlyViewport(t)
	m, rt, _ := menuManager(t, content, true, false)
	rt.afterSend = content
	if !m.HandleLimit(menuEvent(content)) {
		t.Fatal("HandleLimit() did not own menu episode")
	}
	m.Tick(testNow.Add(2 * time.Hour))
	if len(rt.SentKeys) != 1 {
		t.Fatalf("initial sends = %d, want one", len(rt.SentKeys))
	}

	job := m.Snapshot()[0]
	job.State = store.StateValidating
	m.validate(0, job, testNow.Add(2*time.Hour+time.Second))
	if len(rt.SentKeys) != 1 {
		t.Fatalf("sends after second validation = %d, want still one", len(rt.SentKeys))
	}
}

func TestAnswerLimitMenuLogsStillPresentAfterSend(t *testing.T) {
	content := menuValidationContent(readMenuFixture(t, "claude/positive/cc2026-08_menu-team-plan.txt"))
	m, rt, _ := menuManager(t, content, true, false)
	rt.afterSend = content
	var log bytes.Buffer
	m.logw = &log
	if !m.HandleLimit(menuEvent(content)) {
		t.Fatal("HandleLimit() did not own menu episode")
	}
	m.Tick(testNow.Add(2 * time.Hour))
	if len(rt.SentKeys) != 1 || !strings.Contains(log.String(), "menu still present") {
		t.Fatalf("keys=%#v log=%q, want one send and still-present outcome", rt.SentKeys, log.String())
	}
}

func TestAnswerLimitMenuRefusesUnsafeCursorAndContent(t *testing.T) {
	for _, path := range []string{
		"claude/positive/cc2026-08_menu-team-plan.txt",
		"rate_limit_new_format.txt",
	} {
		base := menuValidationContent(readMenuFixture(t, path))
		for _, tc := range []struct {
			name    string
			content string
		}{
			{name: "cursor option 2", content: strings.Replace(base, "❯ 1. Stop and wait for limit to reset", "  1. Stop and wait for limit to reset\n❯ 2. Upgrade your plan", 1)},
			{name: "cursor option 3", content: strings.Replace(base, "❯ 1. Stop and wait for limit to reset", "  1. Stop and wait for limit to reset\n❯ 3. Upgrade to Team plan", 1)},
			{name: "question missing", content: strings.Replace(base, "What do you want to do?", "Which option do you want?", 1)},
			{name: "unrelated menu", content: strings.Replace(base, "Stop and wait for limit to reset", "Continue with another action", 1)},
			{name: "no cursor", content: strings.Replace(base, "❯ 1. Stop and wait for limit to reset", "  1. Stop and wait for limit to reset", 1)},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				m, rt, _ := runMenuValidation(t, tc.content, true, false)
				job := m.Snapshot()[0]
				if len(rt.SentKeys) != 0 || job.MenuAttempt != nil || job.State != store.StateManualRequired {
					t.Fatalf("keys=%#v attempt=%#v state=%s, want no action and manual-required", rt.SentKeys, job.MenuAttempt, job.State)
				}
			})
		}
	}
}

func TestAnswerLimitMenuRestartHonorsPersistedAttempt(t *testing.T) {
	content := menuValidationContent(readMenuFixture(t, "claude/positive/cc2026-08_menu-team-plan.txt"))
	m, rt, st := runMenuValidation(t, content, true, false)
	if len(rt.SentKeys) != 1 {
		t.Fatalf("initial sends=%d, want one", len(rt.SentKeys))
	}
	file := m.Snapshot()
	file[0].State = store.StateValidating
	if err := st.Save(store.File{Version: 1, Jobs: file}); err != nil {
		t.Fatal(err)
	}
	rt2 := &menuRuntime{testRuntime: testRuntime{Fake: runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: "claude", AgentSessionID: "session-1"}},
		Content:   map[string]string{"p1": content},
		Procs:     map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}},
	}}}
	m2 := New(rt2, st, Config{AnswerLimitMenu: true, Margin: time.Minute}, WithClock(func() time.Time { return testNow }), WithSleep(func(time.Duration) {}))
	m2.Tick(testNow.Add(2 * time.Hour))
	if len(rt2.SentKeys) != 0 || m2.Snapshot()[0].MenuAttempt == nil {
		t.Fatalf("restart keys=%#v job=%#v, want persisted attempt honored", rt2.SentKeys, m2.Snapshot()[0])
	}
}

func TestAnswerLimitMenuDryRunLogsWithoutSendingOrExpectingClear(t *testing.T) {
	content := menuValidationContent(readMenuFixture(t, "claude/positive/cc2026-08_menu-team-plan.txt"))
	m, rt, _ := runMenuValidation(t, content, true, true)
	job := m.Snapshot()[0]
	if len(rt.SentKeys) != 0 || job.MenuAttempt == nil || !strings.Contains(job.LastValidation, "dry-run") {
		t.Fatalf("keys=%#v attempt=%#v validation=%q, want log-only dry-run", rt.SentKeys, job.MenuAttempt, job.LastValidation)
	}
}

func TestAnswerLimitMenuOffPreservesManualRequiredNoWrite(t *testing.T) {
	content := menuValidationContent(readMenuFixture(t, "claude/positive/cc2026-08_menu-team-plan.txt"))
	m, rt, _ := runMenuValidation(t, content, false, false)
	job := m.Snapshot()[0]
	if len(rt.SentKeys) != 0 || job.MenuAttempt != nil || job.State != store.StateManualRequired {
		t.Fatalf("keys=%#v attempt=%#v state=%s, want existing fail-closed behavior", rt.SentKeys, job.MenuAttempt, job.State)
	}
}
