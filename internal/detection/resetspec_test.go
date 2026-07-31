package detection

import (
	"testing"
	"time"
)

func TestParseResetTable(t *testing.T) {
	kolkata := time.FixedZone("IST", 5*60*60+30*60)
	base := time.Date(2026, time.July, 31, 12, 0, 0, 0, kolkata)
	tests := []struct {
		name       string
		text       string
		now        time.Time
		kind       ResetKind
		confidence Confidence
		zone       string
		want       time.Time
	}{
		{"local clock", "resets 2pm", base, ResetKindLocalClock, ConfidenceHigh, "", time.Date(2026, 7, 31, 14, 0, 0, 0, kolkata)},
		{"24 hour clock", "resets 15:30", base, ResetKindLocalClock, ConfidenceHigh, "", time.Date(2026, 7, 31, 15, 30, 0, 0, kolkata)},
		{"soonest future", "resets 9", base, ResetKindLocalClock, ConfidenceMedium, "", time.Date(2026, 8, 1, 9, 0, 0, 0, kolkata)},
		{"iana timezone", "resets 3pm (Europe/Amsterdam)", base, ResetKindAbsolute, ConfidenceHigh, "Europe/Amsterdam", time.Date(2026, 7, 31, 15, 0, 0, 0, mustLocation(t, "Europe/Amsterdam"))},
		{"abbreviation", "resets 3pm (ET)", base, ResetKindAbsolute, ConfidenceMedium, "America/New_York", time.Date(2026, 7, 31, 15, 0, 0, 0, mustLocation(t, "America/New_York"))},
		{"unknown timezone", "resets 3pm (MARS)", base, ResetKindLocalClock, ConfidenceLow, "MARS", time.Date(2026, 7, 31, 15, 0, 0, 0, kolkata)},
		{"relative composite", "try again in 2h 30m", base, ResetKindRelative, ConfidenceHigh, "", base.Add(2*time.Hour + 30*time.Minute)},
		{"relative colon", "resets in: 3 hours", base, ResetKindRelative, ConfidenceHigh, "", base.Add(3 * time.Hour)},
		{"explicit date", "resets Oct 9, 10am", base, ResetKindDateTime, ConfidenceHigh, "", time.Date(2026, 10, 9, 10, 0, 0, 0, kolkata)},
		{"weekday", "resets Thursday 3pm", base, ResetKindDateTime, ConfidenceMedium, "", time.Date(2026, 8, 6, 15, 0, 0, 0, kolkata)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseReset(tt.text, tt.now)
			if got.Kind != tt.kind || got.Confidence != tt.confidence || got.Timezone != tt.zone {
				t.Fatalf("spec metadata = %#v, want kind=%q confidence=%q zone=%q", got, tt.kind, tt.confidence, tt.zone)
			}
			if !got.ParsedTime.Equal(tt.want) {
				t.Fatalf("ParsedTime = %v, want %v", got.ParsedTime, tt.want)
			}
		})
	}
}

func TestParseResetGraceAndDST(t *testing.T) {
	loc := mustLocation(t, "Europe/Amsterdam")
	grace := time.Date(2026, 7, 31, 14, 30, 0, 0, loc)
	if got := ParseReset("resets 2pm", grace).ParsedTime; !got.Equal(time.Date(2026, 7, 31, 14, 0, 0, 0, loc)) {
		t.Fatalf("grace ParsedTime = %v", got)
	}
	old := time.Date(2026, 7, 31, 16, 0, 0, 0, loc)
	if got := ParseReset("resets 2pm", old).ParsedTime; !got.Equal(time.Date(2026, 8, 1, 14, 0, 0, 0, loc)) {
		t.Fatalf("next-day ParsedTime = %v", got)
	}
	spring := time.Date(2026, 3, 29, 0, 0, 0, 0, loc)
	if got := ParseReset("resets 2:30am", spring).ParsedTime; !got.Equal(time.Date(2026, 3, 29, 3, 30, 0, 0, loc)) {
		t.Fatalf("spring-gap ParsedTime = %v", got)
	}
	fall := time.Date(2026, 10, 25, 0, 0, 0, 0, loc)
	got := ParseReset("resets 2:30am", fall).ParsedTime
	zone, _ := got.Zone()
	if got.Hour() != 2 || got.Minute() != 30 || zone != "CET" {
		t.Fatalf("fall-back ParsedTime = %v, want later CET occurrence", got)
	}
}

func TestParseResetRejectsImplausibleValues(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	for _, text := range []string{"resets 30", "resets 25:00", "resets in 400 days"} {
		spec := ParseReset(text, now)
		if spec.Kind != ResetKindUnknown || !spec.ParsedTime.IsZero() {
			t.Errorf("ParseReset(%q) = %#v, want unknown zero time", text, spec)
		}
	}
}

func TestParseResetRejectsImpossibleCalendarDates(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, text := range []string{"resets Feb 31, 2026 at 2pm", "resets Apr 31, 2026 at 2pm", "resets Feb 29, 2025 at 2pm"} {
		spec := ParseReset(text, now)
		if spec.Kind != ResetKindUnknown || !spec.ParsedTime.IsZero() {
			t.Errorf("ParseReset(%q) = %#v, want unknown zero time", text, spec)
		}
	}
}

func TestParseResetAcceptsLeapDay(t *testing.T) {
	now := time.Date(2028, 1, 1, 12, 0, 0, 0, time.UTC)
	spec := ParseReset("resets Feb 29, 2028 at 2pm", now)
	if spec.Kind != ResetKindDateTime || spec.ParsedTime.IsZero() || spec.ParsedTime.Month() != time.February || spec.ParsedTime.Day() != 29 {
		t.Fatalf("ParseReset leap day = %#v, want valid February 29", spec)
	}
}

func TestCheckRateLimitAtCarriesTypedResetSpec(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	status := CheckRateLimitAt("You've hit your limit · resets 3pm (Europe/Amsterdam)", now)
	if status.Spec.Kind != ResetKindAbsolute || status.Spec.Timezone != "Europe/Amsterdam" || status.Spec.ParsedTime.IsZero() {
		t.Fatalf("Spec = %#v, want parsed absolute reset", status.Spec)
	}
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}
