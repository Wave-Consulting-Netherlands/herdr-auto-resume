package claude

import (
	"reflect"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
)

func TestClaudeDelegatesDetectionAndPreservesAction(t *testing.T) {
	content := "⎿ You've hit your limit · resets 3pm (UTC)\nOpening your options…\n❯"
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	provider := New("")

	if got, want := provider.DetectContent(content), detection.IsClaudeCode(content); got != want {
		t.Fatalf("DetectContent() = %v, want direct detection result %v", got, want)
	}
	if got, want := provider.Analyze(content, now), detection.Analyze(content, now); !reflect.DeepEqual(got, want) {
		t.Fatalf("Analyze() = %#v, want direct detection result %#v", got, want)
	}
	action := provider.ResumeAction()
	if !reflect.DeepEqual(action.KeysBefore, []string{"escape"}) || action.Text != "continue" || action.SubmitKey != "enter" {
		t.Fatalf("ResumeAction() = %#v, want escape/continue/enter", action)
	}
	if !provider.AllowPeriodicNudge() {
		t.Fatal("Claude AllowPeriodicNudge() = false, want true")
	}
}

func TestClaudePromptOverride(t *testing.T) {
	if got := New("resume this task").ResumeAction().Text; got != "resume this task" {
		t.Fatalf("prompt override = %q, want custom prompt", got)
	}
}

func TestClaudeSafeToResumeMatchesGateNineBody(t *testing.T) {
	content := "╭────╮\n> "
	ok, reason := New("").SafeToResume(content, time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
	if !ok || reason != "validation passed" {
		t.Fatalf("SafeToResume() = %v, %q, want true validation passed", ok, reason)
	}
}
