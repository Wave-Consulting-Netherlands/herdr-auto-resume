package detection

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClassifyFamilyPositiveAndProseNegative(t *testing.T) {
	tests := []struct {
		name     string
		positive string
		negative string
		want     Family
	}{
		{"n-hour", "You've hit your 5-hour limit · resets 3pm", "The 5-hour limit is documented here", FamilyNHourLimit},
		{"session", "You've hit your session limit · resets 2am", "The session limit is described in the README", FamilySessionLimit},
		{"weekly", "You've hit your weekly limit · resets 9am", "The weekly limit is a policy detail", FamilyWeeklyLimit},
		{"usage", "Claude usage limit reached. Resets at 2pm", "The usage limit reached the expected threshold", FamilyUsageLimit},
		{"extra usage", "You're out of extra usage · resets 3pm", "The report says you are out of extra usage", FamilyExtraUsage},
		{"hit", "You've hit your limit · resets 3pm", "The guide says you've hit your limit", FamilyHitLimit},
		{"reached", "Rate limit reached · resets 4pm", "The rate limit reached the server", FamilyLimitReached},
		{"relative", "Please try again in 5 hours", "Please explain how to try again in 5 hours", FamilyRelativeRetry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyFamily(tt.positive); got != tt.want {
				t.Fatalf("ClassifyFamily(positive) = %q, want %q", got, tt.want)
			}
			if got := ClassifyFamily(tt.negative); got != "" {
				t.Fatalf("ClassifyFamily(prose) = %q, want empty", got)
			}
		})
	}
}

func TestPositiveClaudeCorpus(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	want := map[string]struct {
		family     Family
		kind       ResetKind
		timezone   string
		actionable bool
		menu       bool
	}{
		"cc2026-07_5h-iana.txt":        {family: FamilyNHourLimit, kind: ResetKindAbsolute},
		"cc2026-07_session.txt":        {family: FamilySessionLimit, kind: ResetKindAbsolute},
		"cc2026-07_weekly-date.txt":    {family: FamilyWeeklyLimit, kind: ResetKindDateTime},
		"cc2026-07_extra-relative.txt": {family: FamilyExtraUsage, kind: ResetKindRelative},
		"cc2026-07_menu-example.txt":   {family: FamilyHitLimit, kind: ResetKindAbsolute, timezone: "Europe/London", actionable: true, menu: true},
		"cc2026-07_example2.txt":       {family: FamilyLimitReached, kind: ResetKindRelative},
	}
	for name, expected := range want {
		data, err := os.ReadFile(filepath.Join("testdata", "claude", "positive", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		status := CheckRateLimitAt(string(data), now)
		if !status.IsLimited || status.Spec.Kind != expected.kind || status.Spec.ParsedTime.IsZero() {
			t.Errorf("%s status = %#v, want limited parsed %q", name, status, expected.kind)
		}
		if got := ClassifyFamily(string(data)); got != expected.family {
			t.Errorf("%s family = %q, want %q", name, got, expected.family)
		}
		analysis := Analyze(string(data), now)
		if name == "cc2026-07_menu-example.txt" && (analysis.Actionable != expected.actionable || analysis.MenuVisible != expected.menu || analysis.Reset.Timezone != expected.timezone) {
			t.Errorf("%s analysis = %#v, want actionable=%v menu=%v timezone=%q", name, analysis, expected.actionable, expected.menu, expected.timezone)
		}
	}
}
