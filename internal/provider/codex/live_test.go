package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
)

func TestCodexLiveNegativeCorpusNeverActionable(t *testing.T) {
	now := time.Date(2026, 12, 31, 12, 0, 0, 0, codexTestZone)
	paths, err := filepath.Glob(filepath.Join("testdata", "negative", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if analysis := New("").Analyze(string(data), now); analysis.Actionable {
				t.Fatalf("analysis = %#v, want non-actionable", analysis)
			}
		})
	}
}

func TestCodexChromeClassifiersAreLineAnchored(t *testing.T) {
	cases := []string{
		"The guide says › Ask Codex to do anything",
		"The guide says • Working (12s • esc to interrupt)",
		"The guide says ─ Worked for 15m 04s ──",
		"The guide says gpt-5.6-sol · ~/dev/x · Context 90% left · weekly 93% left",
	}
	for _, prose := range cases {
		if IsCodex(prose) {
			t.Errorf("IsCodex(%q) = true, want false", prose)
		}
	}
}

func TestCodexBusyAndStaleTailBlocksSafeResume(t *testing.T) {
	now := time.Date(2026, 12, 31, 12, 0, 0, 0, codexTestZone)
	for _, content := range []string{
		"■ You've hit your usage limit. Try again at 3:51 PM.\n• Working (12s • esc to interrupt)\n› ",
		"■ You've hit your usage limit. Try again at 3:51 PM.\nstream error: exceeded retry limit; retrying 4/5 in 1.471s\n› ",
		"■ You've hit your usage limit. Try again at 3:51 PM.\ncompleted successfully\n› ",
	} {
		analysis := New("").Analyze(content, now)
		if analysis.IsLimited && analysis.Actionable {
			t.Fatalf("analysis = %#v, want blocked", analysis)
		}
		if ok, _ := New("").SafeToResume(content, now); ok {
			t.Fatalf("SafeToResume() = true for guarded content %q", content)
		}
	}
}

func TestCodexWrappedUsageBannerJoinsResetTail(t *testing.T) {
	now := time.Date(2026, 12, 31, 12, 0, 0, 0, codexTestZone)
	const prefix = "■ You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or "
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "split before at",
			content: prefix + "try again\n at 3:34 PM.\n› ",
			want:    prefix + "try again at 3:34 PM.",
		},
		{
			name:    "split mid URL",
			content: "■ You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usa\nge to purchase more credits or try again at 3:34 PM.\n› ",
			want:    "■ You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usa ge to purchase more credits or try again at 3:34 PM.",
		},
		{
			name:    "three continuation lines",
			content: "■ You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or\ntry\nagain\n at 3:34 PM.\n› ",
			want:    "■ You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 3:34 PM.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			analysis := New("").Analyze(tc.content, now)
			if !analysis.IsLimited || !analysis.Actionable || analysis.Family != FamilyUsageLimit || analysis.Reset.Kind != detection.ResetKindLocalClock || analysis.Reset.ParsedTime.IsZero() {
				t.Fatalf("analysis = %#v, want actionable local-clock limit", analysis)
			}
			if analysis.Evidence != tc.want {
				t.Fatalf("Evidence = %q, want %q", analysis.Evidence, tc.want)
			}
		})
	}
}

func TestCodexWrappedUsageBannerStopsAtBlankLine(t *testing.T) {
	now := time.Date(2026, 12, 31, 12, 0, 0, 0, codexTestZone)
	banner := "■ You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or"
	content := banner + "\n\ntry again at 5:00 PM. This is unrelated prose.\n› "
	analysis := New("").Analyze(content, now)
	if !analysis.IsLimited || analysis.Actionable || analysis.Reset.Kind != detection.ResetKindUnknown {
		t.Fatalf("analysis = %#v, want non-actionable unknown limit", analysis)
	}
	if strings.Contains(analysis.Evidence, "try again at 5:00 PM") || analysis.Evidence != banner {
		t.Fatalf("Evidence = %q, want only banner before blank line", analysis.Evidence)
	}
}
