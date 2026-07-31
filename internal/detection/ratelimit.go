package detection

import (
	"regexp"
	"time"
)

// RateLimitStatus represents the rate limit state of a pane
type RateLimitStatus struct {
	IsLimited bool
	ResetsAt  string    // Original string like "2pm" or "10:30am"
	ResetTime time.Time // Parsed reset time
	TimeUntil time.Duration
	Spec      ResetSpec
}

// Rate limit patterns - multiple formats Claude Code uses
// Examples: "limit reached ∙ resets 2pm", "limit reached ∙ resets 10:30am"
//
//	"You've hit your limit · resets 10pm (Europe/London)"
//	"Limit reached (resets 8m)" - minutes remaining format
var rateLimitPatterns = []*regexp.Regexp{
	// New format: "You've hit your limit · resets 10pm (Europe/London)"
	regexp.MustCompile(`(?i)hit\s+your\s+limit.*resets?\s+(\d{1,2}(?::\d{2})?\s*[ap]m)`),
	// Original format: "limit reached ∙ resets 2pm"
	regexp.MustCompile(`(?i)limit\s+reached.*resets?\s+(\d{1,2}(?::\d{2})?\s*[ap]m)`),
	// Minutes remaining format: "Limit reached (resets 8m)" or "resets 45m"
	regexp.MustCompile(`(?i)(?:hit\s+your\s+limit|limit\s+reached).*resets?\s+(\d{1,3})m\b`),
}

// Fallback patterns - detect rate limit without capturing time
// Used when we can't parse a specific reset time
// These patterns are more specific to avoid false positives
var rateLimitFallbackPatterns = []*regexp.Regexp{
	// "You've hit your limit" - Claude Code's primary message
	regexp.MustCompile(`(?i)you['']ve\s+hit\s+your\s+limit`),
	// "Limit reached" at word boundary (not "rate limit exceeded" or similar)
	regexp.MustCompile(`(?i)\blimit\s+reached\b`),
	// "rate limited" as a status indicator
	regexp.MustCompile(`(?i)\brate\s+limited\b`),
}

// CheckRateLimit checks pane content for rate limit messages
func CheckRateLimit(content string) RateLimitStatus {
	return CheckRateLimitAt(content, time.Now())
}

// CheckRateLimitAt checks pane content using the supplied clock and timezone.
// The explicit clock keeps parsing deterministic for schedulers and tests.
func CheckRateLimitAt(content string, now time.Time) RateLimitStatus {
	// Try patterns that capture reset time first
	var match []string
	var patternIdx int
	for i, pattern := range rateLimitPatterns {
		match = pattern.FindStringSubmatch(content)
		if match != nil {
			patternIdx = i
			break
		}
	}

	limited := match != nil
	// If no time-capturing pattern matched, try fallback patterns.
	if match == nil {
		for _, pattern := range rateLimitFallbackPatterns {
			if pattern.MatchString(content) {
				limited = true
				break
			}
		}
	}
	if !limited {
		return RateLimitStatus{Spec: ResetSpec{Kind: ResetKindUnknown, Confidence: ConfidenceLow}}
	}

	spec := ParseReset(content, now)
	resetStr := ""
	if match != nil {
		resetStr = match[1]
	}
	if spec.ParsedTime.IsZero() && resetStr != "" {
		spec = ParseReset("resets "+resetStr, now)
	}
	status := RateLimitStatus{
		IsLimited: true,
		ResetsAt:  resetStr,
		Spec:      spec,
	}
	if patternIdx == 2 {
		status.ResetsAt = resetStr + "m"
	}
	if status.ResetsAt == "" && !spec.ParsedTime.IsZero() {
		status.ResetsAt = spec.Raw
	}
	if !spec.ParsedTime.IsZero() {
		status.ResetTime = spec.ParsedTime
		status.TimeUntil = spec.ParsedTime.Sub(now)
	}
	return status
}

// HasReset checks if the rate limit has reset (time has passed)
func (r RateLimitStatus) HasReset() bool {
	return r.HasResetAt(time.Now())
}

// HasResetAt reports whether the supplied clock is strictly after the reset.
func (r RateLimitStatus) HasResetAt(now time.Time) bool {
	if !r.IsLimited {
		return false
	}
	if r.ResetTime.IsZero() {
		return false
	}
	return now.After(r.ResetTime)
}
