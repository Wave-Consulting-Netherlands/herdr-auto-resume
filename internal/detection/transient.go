package detection

import (
	"regexp"
	"strings"
)

// TransientClass identifies a non-reset-bearing provider/API failure.
type TransientClass string

const (
	TransientHTTP429             TransientClass = "http-429"
	TransientHTTP5xx             TransientClass = "http-5xx"
	TransientOverloaded          TransientClass = "overloaded"
	TransientTemporarilyLimiting TransientClass = "temporarily-limiting-requests"
	TransientConnection          TransientClass = "connection-error"
)

// TransientPatternInfo exposes the intentionally replaceable hypothesis table
// for diagnostics and capture-drilling tools. None of these patterns has a
// real pane capture in this repository yet.
type TransientPatternInfo struct {
	Class      TransientClass
	Expression string
	Provenance string
}

// transientPattern is the one hypothesis table for transient detection. The
// exact source for every entry is BACKLOG item 16 / the task brief: it names
// the failure wording but provides no captured pane output or provider-doc URL.
// Therefore each entry is an unverified guess and must be replaced or narrowed
// when scripts/limit-capture.sh produces a real fixture.
var transientPatternTable = []struct {
	class      TransientClass
	pattern    *regexp.Regexp
	provenance string
}{
	{TransientHTTP429, regexp.MustCompile(`(?i)\b(?:429|too\s+many\s+requests)\b`), "unverified guess: BACKLOG item 16/task brief; no real pane capture"},
	{TransientHTTP5xx, regexp.MustCompile(`(?i)\b(?:5\d\d|5xx)\b`), "unverified guess: BACKLOG item 16/task brief; no real pane capture"},
	{TransientOverloaded, regexp.MustCompile(`(?i)\boverloaded\b`), "unverified guess: BACKLOG item 16/task brief; no real pane capture"},
	{TransientTemporarilyLimiting, regexp.MustCompile(`(?i)\btemporarily\s+limiting\s+requests\b`), "unverified guess: BACKLOG item 16/task brief; no real pane capture"},
	{TransientConnection, regexp.MustCompile(`(?i)\b(?:api\s+error\s*:\s*)?(?:connection(?:\s+(?:reset|refused|closed|failed|error))?|network\s+error)\b`), "unverified guess: BACKLOG item 16/task brief; no real pane capture"},
}

// ClassifyTransient applies the hypothesis table in declaration order.
func ClassifyTransient(content string) (TransientPatternInfo, bool) {
	for _, candidate := range transientPatternTable {
		if candidate.pattern.MatchString(content) {
			return TransientPatternInfo{
				Class:      candidate.class,
				Expression: candidate.pattern.String(),
				Provenance: candidate.provenance,
			}, true
		}
	}
	return TransientPatternInfo{}, false
}

// TransientMatchRegex returns the event-subscription expression derived from
// the same hypothesis table used by ClassifyTransient. Keep callers from
// maintaining a second, inevitably divergent pattern list.
func TransientMatchRegex() string {
	parts := make([]string, 0, len(transientPatternTable))
	for _, candidate := range transientPatternTable {
		parts = append(parts, candidate.pattern.String())
	}
	return "(?:" + strings.Join(parts, "|") + ")"
}

// TransientPatterns returns a copy of the current hypothesis metadata.
func TransientPatterns() []TransientPatternInfo {
	patterns := make([]TransientPatternInfo, 0, len(transientPatternTable))
	for _, candidate := range transientPatternTable {
		patterns = append(patterns, TransientPatternInfo{
			Class: candidate.class, Expression: candidate.pattern.String(), Provenance: candidate.provenance,
		})
	}
	return patterns
}
