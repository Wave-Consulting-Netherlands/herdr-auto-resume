package codex

import (
	"regexp"
	"strings"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
)

var tryAgainPattern = regexp.MustCompile(`(?i)(?:or\s+)?try\s+again\b`)
var ordinalPattern = regexp.MustCompile(`(?i)\b(\d+)(?:st|nd|rd|th)\b`)

// normalizeResetTail converts only Codex's documented reset tails into the
// syntax accepted by detection.ParseReset.
func normalizeResetTail(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	match := tryAgainPattern.FindStringIndex(text)
	if match == nil {
		return ""
	}
	tail := strings.TrimSpace(text[match[1]:])
	tail = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tail), "at "))
	tail = ordinalPattern.ReplaceAllString(tail, "$1")
	return strings.TrimSpace(strings.TrimRight(tail, "."))
}

func parseCodexReset(text string, now time.Time) detection.ResetSpec {
	tail := normalizeResetTail(text)
	if tail == "" || strings.EqualFold(tail, "later") {
		return detection.ResetSpec{Kind: detection.ResetKindUnknown, Raw: tail, Confidence: detection.ConfidenceLow}
	}
	return detection.ParseReset(tail, now)
}
