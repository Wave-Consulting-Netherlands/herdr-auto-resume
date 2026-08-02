package coordinator

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

type diagnosticProvider struct {
	name     string
	detect   bool
	analysis detection.Analysis
}

func (p *diagnosticProvider) Name() string { return p.name }

func (p *diagnosticProvider) DetectContent(string) bool { return p.detect }

func (p *diagnosticProvider) Analyze(string, time.Time) detection.Analysis { return p.analysis }

func (p *diagnosticProvider) SafeToResume(string, time.Time) (bool, string) { return false, "" }

func (p *diagnosticProvider) ResumeAction() provider.ResumeAction { return provider.ResumeAction{} }

func (p *diagnosticProvider) AllowPeriodicNudge() bool { return false }

func diagnosticAnalysis(now time.Time, evidence, reset string, actionable, menu bool) detection.Analysis {
	spec := detection.ResetSpec{Raw: reset, Kind: detection.ResetKindUnknown, Confidence: detection.ConfidenceLow}
	if reset != "" && reset != "later" {
		spec.Kind = detection.ResetKindRelative
		spec.ParsedTime = now.Add(time.Hour)
		spec.Confidence = detection.ConfidenceHigh
	}
	return detection.Analysis{
		IsLimited:   true,
		Actionable:  actionable,
		MenuVisible: menu,
		Reset:       spec,
		Evidence:    evidence,
	}
}

func diagnosticLines(log string) []string {
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func TestLimitedPaneDiagnosticsLogEachNonActionReasonOncePerEvidence(t *testing.T) {
	now := coordinatorTestNow
	cases := []struct {
		name       string
		reason     string
		actionable bool
		menu       bool
		reset      string
		modeAuto   bool
		owned      bool
	}{
		{name: "not auto", reason: "not-auto", actionable: true, reset: "2pm"},
		{name: "menu visible", reason: "menu-visible", actionable: false, menu: true, reset: "2pm", modeAuto: true},
		{name: "not actionable", reason: "not-actionable", actionable: false, reset: "2pm", modeAuto: true},
		{name: "reset unparsed", reason: "reset-unparsed", actionable: true, reset: "later", modeAuto: true},
		{name: "job manager declined", reason: "job-manager-declined", actionable: true, reset: "2pm", modeAuto: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &diagnosticProvider{
				name:     "claude",
				detect:   true,
				analysis: diagnosticAnalysis(now, "evidence-1", tc.reset, tc.actionable, tc.menu),
			}
			fake := &runtime.Fake{
				PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}},
				Content:   map[string]string{"p1": "limited"},
			}
			sink := &recordingJobSink{owned: tc.owned}
			var log bytes.Buffer
			c := New(fake, Config{},
				WithClock(func() time.Time { return now }),
				WithProviders(provider.NewRegistry(p)),
				WithJobSink(sink),
				WithLogWriter(&log),
			)
			c.SetPanes(fake.PanesList)
			c.Poll()
			if tc.modeAuto {
				c.EnableAll()
			}
			c.Poll()
			c.Poll()
			p.analysis.Evidence = "evidence-2"
			c.Poll()

			lines := diagnosticLines(log.String())
			if len(lines) != 2 {
				t.Fatalf("diagnostic lines = %d (%q), want one per evidence hash", len(lines), log.String())
			}
			for _, line := range lines {
				if !strings.Contains(line, "pane=p1") || !strings.Contains(line, "provider=claude") || !strings.Contains(line, "reason="+tc.reason) {
					t.Fatalf("diagnostic line = %q, want pane/provider/reason", line)
				}
				if !strings.Contains(line, "reset=\""+tc.reset+"\"") {
					t.Fatalf("diagnostic line = %q, want raw reset %q", line, tc.reset)
				}
			}
		})
	}
}

func TestLimitedPaneWithUnresolvedProviderLogsOnceAndChangedEvidenceAgain(t *testing.T) {
	now := coordinatorTestNow
	fake := &runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: "unknown"}},
		Content:   map[string]string{"p1": "You've hit your limit · resets 2pm"},
	}
	var log bytes.Buffer
	c := New(fake, Config{},
		WithClock(func() time.Time { return now }),
		WithProviders(provider.NewRegistry(&diagnosticProvider{name: "claude"})),
		WithLogWriter(&log),
	)
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.Poll()
	fake.Content["p1"] = "Rate limit reached · resets 3pm"
	c.Poll()

	lines := diagnosticLines(log.String())
	if len(lines) != 2 {
		t.Fatalf("diagnostic lines = %d (%q), want one per unresolved evidence hash", len(lines), log.String())
	}
	for _, line := range lines {
		if !strings.Contains(line, "pane=p1") || !strings.Contains(line, "provider=none") || !strings.Contains(line, "reason=provider-unresolved") {
			t.Fatalf("diagnostic line = %q, want unresolved pane/provider/reason", line)
		}
	}
}

func TestAmbiguousProviderLimitLogsAsUnresolved(t *testing.T) {
	now := coordinatorTestNow
	content := "You've hit your limit · resets 2pm"
	first := &diagnosticProvider{name: "first", detect: true}
	second := &diagnosticProvider{name: "second", detect: true}
	fake := &runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1"}},
		Content:   map[string]string{"p1": content},
	}
	var log bytes.Buffer
	c := New(fake, Config{},
		WithClock(func() time.Time { return now }),
		WithProviders(provider.NewRegistry(first, second)),
		WithLogWriter(&log),
	)
	c.SetPanes(fake.PanesList)
	c.Poll()

	lines := diagnosticLines(log.String())
	if len(lines) != 1 || !strings.Contains(lines[0], "provider=none") || !strings.Contains(lines[0], "reason=provider-unresolved") {
		t.Fatalf("diagnostic lines = %#v, want one ambiguous-provider record", lines)
	}
}

func TestLimitedPaneThatCreatesJobEmitsNoDiagnostic(t *testing.T) {
	now := coordinatorTestNow
	p := &diagnosticProvider{
		name:     "claude",
		detect:   true,
		analysis: diagnosticAnalysis(now, "evidence-1", "2pm", true, false),
	}
	fake := &runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}},
		Content:   map[string]string{"p1": "limited"},
	}
	sink := &recordingJobSink{owned: true}
	var log bytes.Buffer
	c := New(fake, Config{},
		WithClock(func() time.Time { return now }),
		WithProviders(provider.NewRegistry(p)),
		WithJobSink(sink),
		WithLogWriter(&log),
	)
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.EnableAll()
	c.Poll()

	if len(sink.events) != 1 {
		t.Fatalf("job events = %d, want one", len(sink.events))
	}
	if got := log.String(); got != "" {
		t.Fatalf("diagnostic log = %q, want empty for job-created happy path", got)
	}
}
