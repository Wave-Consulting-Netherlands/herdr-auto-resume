package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
