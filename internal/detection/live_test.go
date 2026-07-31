package detection

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAnalyzeLiveAndSafetyGuards(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	live := "Claude Code\n⎿ You've hit your limit · resets 3pm (UTC)\nOpening your options…\n❯"
	analysis := Analyze(live, now)
	if !analysis.IsLimited || !analysis.Actionable || analysis.Reset.ParsedTime.IsZero() || analysis.Evidence == "" {
		t.Fatalf("live analysis = %#v, want actionable parsed evidence", analysis)
	}
	if analysis.MenuVisible {
		t.Fatal("plain live banner unexpectedly has a menu")
	}

	menu := live + "\nWhat do you want to do?\n❯ 1. Stop and wait for limit to reset\n2. Upgrade\nEnter to confirm · Esc to cancel"
	if analysis := Analyze(menu, now); !analysis.MenuVisible || !analysis.Actionable {
		t.Fatalf("menu analysis = %#v, want visible actionable menu", analysis)
	}
	stale := live + "\nnormal agent output below the banner"
	if analysis := Analyze(stale, now); analysis.Actionable {
		t.Fatalf("stale analysis = %#v, want non-actionable", analysis)
	}
	quoted := "```\nYou've hit your limit · resets 3pm (UTC)\n```\n> limit reached · resets 4pm"
	if analysis := Analyze(quoted, now); analysis.Actionable {
		t.Fatalf("quoted analysis = %#v, want non-actionable", analysis)
	}
	toolEcho := "⏺ Bash(git status)\n  ⎿ You've hit your limit · resets 3pm (UTC)"
	if analysis := Analyze(toolEcho, now); analysis.Actionable {
		t.Fatalf("tool echo analysis = %#v, want non-actionable", analysis)
	}
	child := "⎿ You've hit your limit · resets 3pm (UTC)"
	if analysis := Analyze(child, now); !analysis.Actionable {
		t.Fatalf("unheaded child analysis = %#v, want live exception", analysis)
	}
}

func TestHasRateLimitMenuAndIdlePrompt(t *testing.T) {
	menu := "What do you want to do?\n❯ 1. Stop and wait for limit to reset\n2. Upgrade\nEnter to confirm · Esc to cancel"
	if !HasRateLimitMenu(menu) {
		t.Fatal("HasRateLimitMenu() = false, want true")
	}
	if HasRateLimitMenu("What do you want to do?\n1. A normal numbered list\nPress Enter later") {
		t.Fatal("HasRateLimitMenu() = true for prose probe")
	}
	if !IsIdlePrompt("status\n❯") {
		t.Fatal("bare ❯ prompt should be idle")
	}
	if IsIdlePrompt(menu) {
		t.Fatal("menu selector should not be idle")
	}
}

func TestChromeClassifiersHaveRenderAnchors(t *testing.T) {
	tests := []struct {
		name     string
		positive string
		negative string
	}{
		{"box rule", "────────────────────", "Use ──────────────────── in prose"},
		{"boxed input", "│ > continue │", "│ users │ orders │"},
		{"footer", "⏵⏵ auto mode on", "The ⏵⏵ marker is documented"},
		{"shortcut", "? for shortcuts", "Press ctrl+c to stop the dev server"},
		{"version", "| v2.1.220", "Released v0.5.1"},
		{"usage", "[Opus 4.5 | Max] ████", "The [Opus 4.5 | Max] example is prose"},
		{"tally", "✓ Bash ×10 | ✓ Read ×4", "✓ Fixed the bug"},
		{"todos", "✓ All todos complete (4/4)", "✓ Fixed the bug"},
		{"tasks", "3 tasks (2 completed)", "We discussed 3 tasks (2 completed)"},
		{"completed", "+3 completed", "The release added +3 completed items"},
		{"spinner", "✻ Marinating… (9m)", "The star ✻ is decorative"},
		{"options", "Opening your options…", "Opening your options in the guide"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !isChromeLine(tt.positive) {
				t.Fatalf("positive %q was not chrome", tt.positive)
			}
			if isChromeLine(tt.negative) {
				t.Fatalf("prose %q was chrome", tt.negative)
			}
		})
	}
}

func TestNegativeClaudeCorpus(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	entries, err := os.ReadDir(filepath.Join("testdata", "claude", "negative"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join("testdata", "claude", "negative", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		analysis := Analyze(string(data), now)
		if analysis.Actionable || (analysis.IsLimited && analysis.Actionable && !analysis.Reset.ParsedTime.IsZero()) {
			t.Errorf("negative %s analysis = %#v, want non-actionable", entry.Name(), analysis)
		}
	}
}
