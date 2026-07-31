package detection

import (
	"regexp"
	"strings"
)

type Family string

const (
	FamilyNHourLimit    Family = "n-hour-limit"
	FamilySessionLimit  Family = "session-limit"
	FamilyWeeklyLimit   Family = "weekly-limit"
	FamilyUsageLimit    Family = "usage-limit"
	FamilyExtraUsage    Family = "extra-usage"
	FamilyHitLimit      Family = "hit-limit"
	FamilyLimitReached  Family = "limit-reached"
	FamilyRelativeRetry Family = "relative-retry"
)

var familyPatterns = []struct {
	family  Family
	pattern *regexp.Regexp
}{
	{FamilyNHourLimit, regexp.MustCompile(`(?im)^\s*(?:⎿\s*)?(?:you['’]ve\s+hit\s+your\s+)?\d+-hour\s+limit(?:\s+(?:reached|hit))?\b`)},
	{FamilySessionLimit, regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?(?:you['’]ve\s+hit\s+your\s+)?session\s+limit(?:\s+(?:reached|hit))?\b`)},
	{FamilyWeeklyLimit, regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?(?:you['’]ve\s+hit\s+your\s+)?weekly\s+limit(?:\s+(?:reached|hit))?\b`)},
	{FamilyExtraUsage, regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?(?:you['’]re\s+)?out\s+of\s+(?:extra\s+)?usage\b`)},
	{FamilyUsageLimit, regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?claude\s+usage\s+limit\s+reached\b`)},
	{FamilyHitLimit, regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?you['’]ve\s+hit\s+your\s+(?:\w+\s+){0,2}limit\b`)},
	{FamilyLimitReached, regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?(?:rate\s+)?limit\s+reached\b`)},
	{FamilyRelativeRetry, regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?(?:please\s+)?try\s+again\s+in\b`)},
}

var broadRateLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?you['’]ve\s+hit\s+your\s+(?:\w+\s+){0,3}limit\b`),
	regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?(?:you['’]ve\s+hit\s+your\s+)?\d+-hour\s+limit\b`),
	regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?(?:claude\s+)?usage\s+limit\b`),
	regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?(?:you['’]re\s+)?out\s+of\s+(?:extra\s+)?usage\b`),
	regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?(?:rate\s+)?limit\s+(?:reached|hit)\b`),
	regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?rate\s+limited\b`),
	regexp.MustCompile(`(?im)^\s*(?:⎿\s*|⚠\s*)?(?:please\s+)?try\s+again\s+in\s+\d+\s*(?:hours?|minutes?|h|m)\b`),
}

func ClassifyFamily(content string) Family {
	for _, candidate := range familyPatterns {
		if candidate.pattern.MatchString(content) {
			return candidate.family
		}
	}
	return ""
}

func isRateLimitSignal(content string) bool {
	for _, pattern := range broadRateLimitPatterns {
		if pattern.MatchString(content) {
			return true
		}
	}
	return false
}

func legacyResetText(raw string) string {
	if i := strings.LastIndex(raw, "("); i >= 0 && strings.HasSuffix(raw, ")") {
		return strings.TrimSpace(raw[:i])
	}
	return strings.TrimSpace(raw)
}
