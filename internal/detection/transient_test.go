package detection

import (
	"testing"
	"time"
)

func TestClassifyTransientHypothesisTable(t *testing.T) {
	cases := []struct {
		name    string
		content string
		class   TransientClass
	}{
		{name: "429", content: "API error 429: too many requests", class: TransientHTTP429},
		{name: "5xx", content: "API error: 503 service unavailable", class: TransientHTTP5xx},
		{name: "overloaded", content: "The service is overloaded", class: TransientOverloaded},
		{name: "temporarily limiting", content: "temporarily limiting requests", class: TransientTemporarilyLimiting},
		{name: "connection", content: "api error: connection reset", class: TransientConnection},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			match, ok := ClassifyTransient(tc.content)
			if !ok || match.Class != tc.class {
				t.Fatalf("ClassifyTransient(%q) = %#v, %v; want class %q", tc.content, match, ok, tc.class)
			}
			analysis := Analyze(tc.content, time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
			if !analysis.Transient || analysis.TransientClass != tc.class || analysis.IsLimited {
				t.Fatalf("Analyze(%q) = %#v, want transient-only class %q", tc.content, analysis, tc.class)
			}
		})
	}
}

func TestResetBearingLimitBeatsTransient(t *testing.T) {
	content := "You've hit your limit · resets 2pm\nAPI error: connection reset"
	analysis := Analyze(content, time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
	if !analysis.IsLimited || !analysis.Actionable || analysis.Reset.ParsedTime.IsZero() {
		t.Fatalf("analysis = %#v, want reset-bearing limit", analysis)
	}
	if analysis.Transient {
		t.Fatalf("analysis = %#v, want transient suppressed by reset-bearing limit", analysis)
	}
}
