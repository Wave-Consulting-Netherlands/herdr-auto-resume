package codex

import (
	"testing"
	"time"
)

func TestNormalizeCodexResetTail(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"same day", "You've hit your usage limit. or try again at 3:51 PM.", "3:51 PM"},
		{"ordinal", "or try again at Feb 23rd, 2027 9:01 PM.", "Feb 23, 2027 9:01 PM"},
		{"relative", "Try again in 4 days 20 hours 9 minutes.", "in 4 days 20 hours 9 minutes"},
		{"later", "Try again later.", "later"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeResetTail(tc.text); got != tc.want {
				t.Fatalf("normalizeResetTail() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseCodexCrossDayAcrossYearBoundary(t *testing.T) {
	zone := time.FixedZone("fixture-zone", 5*60*60+30*60)
	now := time.Date(2026, 12, 31, 23, 0, 0, 0, zone)
	spec := parseCodexReset("or try again at Jan 1st, 2027 12:00 AM.", now)
	want := time.Date(2027, 1, 1, 0, 0, 0, 0, zone)
	if spec.ParsedTime.IsZero() || !spec.ParsedTime.Equal(want) {
		t.Fatalf("spec = %#v, want %v", spec, want)
	}
}
