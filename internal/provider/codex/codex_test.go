package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
)

var codexTestZone = time.FixedZone("fixture-zone", -3*60*60)

func TestPositiveCodexCorpus(t *testing.T) {
	now := time.Date(2026, 12, 31, 12, 0, 0, 0, codexTestZone)
	want := map[string]struct {
		family detection.Family
		kind   detection.ResetKind
	}{
		"codex0.146_at-sameday.txt":          {FamilyUsageLimit, detection.ResetKindLocalClock},
		"codex0.146_at-crossday-ordinal.txt": {FamilyUsageLimit, detection.ResetKindDateTime},
		"codex0.146_model-switch.txt":        {FamilyModelUsageLimit, detection.ResetKindLocalClock},
		"codex0.146_pro-upgrade.txt":         {FamilyUsageLimit, detection.ResetKindLocalClock},
		"codex0.146_plus-upgrade.txt":        {FamilyUsageLimit, detection.ResetKindLocalClock},
		"codex0.146_admin-request.txt":       {FamilyUsageLimit, detection.ResetKindDateTime},
		"codex0.146_later-noreset.txt":       {FamilyUsageLimit, detection.ResetKindUnknown},
		"codex0.146_credits.txt":             {FamilyCredits, detection.ResetKindUnknown},
		"codex0.146_spendcap.txt":            {FamilySpendCap, detection.ResetKindUnknown},
		"codex0.144_relative-multiunit.txt":  {FamilyUsageLimit, detection.ResetKindRelative},
	}
	for name, expected := range want {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "positive", name))
			if err != nil {
				t.Fatal(err)
			}
			analysis := New("").Analyze(string(data), now)
			if !analysis.IsLimited || analysis.Family != expected.family || analysis.Reset.Kind != expected.kind {
				t.Fatalf("analysis = %#v, want limited family=%q kind=%q", analysis, expected.family, expected.kind)
			}
			if expected.kind == detection.ResetKindUnknown {
				if analysis.Actionable || !analysis.Reset.ParsedTime.IsZero() {
					t.Fatalf("no-reset analysis = %#v, want non-actionable zero time", analysis)
				}
			} else if analysis.Reset.ParsedTime.IsZero() {
				t.Fatalf("analysis reset = %#v, want parsed time", analysis.Reset)
			}
		})
	}
}

func TestCodexIdentityCorpus(t *testing.T) {
	p := New("")
	positive, err := filepath.Glob(filepath.Join("testdata", "positive", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range positive {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !p.DetectContent(string(data)) {
			t.Errorf("DetectContent(%s) = false, want true", path)
		}
	}
	negative := []string{
		filepath.Join("testdata", "negative", "claude_chrome.txt"),
		filepath.Join("testdata", "negative", "shell.txt"),
		filepath.Join("testdata", "negative", "psql_tables.txt"),
	}
	for _, path := range negative {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if p.DetectContent(string(data)) {
			t.Errorf("DetectContent(%s) = true, want false", path)
		}
	}
}

func TestCodexActionAndSafetyDefaults(t *testing.T) {
	p := New("")
	action := p.ResumeAction()
	if len(action.KeysBefore) != 0 || action.Text != defaultPrompt || action.SubmitKey != "enter" {
		t.Fatalf("ResumeAction() = %#v, want no keys/default prompt/enter", action)
	}
	if p.AllowPeriodicNudge() {
		t.Fatal("AllowPeriodicNudge() = true, want false")
	}
	if ok, _ := p.SafeToResume("■ You've hit your usage limit. Try again later.\n› ", time.Now()); ok {
		t.Fatal("SafeToResume() = true for no-reset limit")
	}
}
