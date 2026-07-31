package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestParseRunFlagsAcceptsProviderOptions(t *testing.T) {
	var stderr bytes.Buffer
	cfg, err := parseRunFlags([]string{"--pane", "w1:p1", "--providers", "codex,claude", "--claude-prompt", "continue now", "--codex-prompt", "resume Codex"}, &stderr)
	if err != nil || cfg.Providers != "codex,claude" || cfg.ClaudePrompt != "continue now" || cfg.CodexPrompt != "resume Codex" {
		t.Fatalf("config=%#v err=%v", cfg, err)
	}
}

func TestParseRunFlagsRejectsUnknownProvider(t *testing.T) {
	var stderr bytes.Buffer
	if _, err := parseRunFlags([]string{"--pane", "w1:p1", "--providers", "claude,gemini"}, &stderr); err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("error = %v, want unsupported provider", err)
	}
}

func TestDetectCommandCodexGoldens(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{name: "positive", data: "■ You've hit your usage limit. Try again at 3:51 PM.\n› Ask Codex to do anything\n", want: "IsLimited=true"},
		{name: "headsup negative", data: "Heads up, you have less than 10% of your weekly limit left. Run /status for a breakdown.\n› Ask Codex to do anything\n", want: "IsLimited=false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.txt")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if got := runCLI([]string{"detect", "--provider", "codex", "--file", path}, &stdout, &stderr); got != 0 {
				t.Fatalf("detect exit = %d stderr=%q", got, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.want) || stderr.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestStatusCommandIncludesProviderColumn(t *testing.T) {
	path := writeCommandState(t)
	file, err := store.NewJSONStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	file.Jobs[0].Provider = "codex"
	if err := store.NewJSONStore(path).Save(file); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if got := jobCommand([]string{"status", "--state-file", path}, &out, &errOut); got != 0 {
		t.Fatalf("status exit = %d stderr=%q", got, errOut.String())
	}
	if !strings.Contains(out.String(), "PROVIDER") || !strings.Contains(out.String(), "codex") {
		t.Fatalf("status output = %q", out.String())
	}
}

func TestCodexFakeRuntimeCycleThroughCLIComponents(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	content := "■ You've hit your usage limit. Try again at 3:51 PM.\n› Ask Codex to do anything"
	rt := &runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "codex"}}, Content: map[string]string{"p1": content}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "codex", CWD: "/work"}}}
	registry := provider.NewRegistry(claude.New(""), codex.New(""))
	st := store.NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	m := jobs.New(rt, st, jobs.Config{Margin: time.Minute}, jobs.WithProviders(registry), jobs.WithClock(func() time.Time { return now }), jobs.WithSleep(func(time.Duration) {}), jobs.WithIDGenerator(func() string { return "job-1" }))
	c := coordinator.New(rt, coordinator.Config{ReadLines: 20}, coordinator.WithProviders(registry), coordinator.WithClock(func() time.Time { return now }), coordinator.WithJobSink(m), coordinator.WithSleep(func(time.Duration) {}))
	c.SetPanes(rt.PanesList)
	c.Poll()
	c.EnableAll()
	c.Poll()
	job := m.Snapshot()[0]
	m.Tick(job.ResumeAtUTC)
	if len(rt.SentText) != 1 || len(rt.SentKeys) != 1 || rt.SentKeys[0].Keys[0] != "enter" {
		t.Fatalf("writes = text %#v keys %#v, want one prompt and Enter", rt.SentText, rt.SentKeys)
	}
	rt.Content["p1"] = "› Ask Codex to do anything"
	m.Tick(job.ResumeAtUTC.Add(time.Second))
	if got := m.Snapshot()[0].State; got != store.StateResumed {
		t.Fatalf("state = %s, want RESUMED", got)
	}
}
