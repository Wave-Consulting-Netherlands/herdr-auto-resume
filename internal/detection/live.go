package detection

import (
	"regexp"
	"strings"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/terminal"
)

type Analysis struct {
	IsLimited      bool
	Actionable     bool
	MenuVisible    bool
	Family         Family
	Reset          ResetSpec
	Evidence       string
	Transient      bool
	TransientClass TransientClass
}

var (
	boxRuleLine     = regexp.MustCompile(`^[─│┃┌┐└┘├┤┬┴┼╭╮╯╰╌]+$`)
	menuOptionLine  = regexp.MustCompile(`^❯?\s*\d+\.\s+`)
	resetMarkerLine = regexp.MustCompile(`(?i)\b(?:resets?|try\s+again\s+in|wait)\b`)
)

// Analyze applies the live-tail and quote guards around the raw rate-limit
// predicate. Raw IsLimited remains useful to status displays; Actionable is
// the stricter predicate used before scheduling or sending input.
func Analyze(content string, now time.Time) Analysis {
	raw := CheckRateLimitAt(content, now)
	analysis := Analysis{IsLimited: raw.IsLimited, Reset: raw.Spec}
	lines := visibleTail(content)
	analysis.MenuVisible = hasMenuLines(lines)
	if limit, reset, ok := pairedEvidence(lines); ok {
		pair := strings.Join(lines[limit:reset+1], "\n")
		status := CheckRateLimitAt(pair, now)
		analysis.IsLimited = true
		analysis.Reset = status.Spec
		analysis.Family = ClassifyFamily(pair)
		analysis.Evidence = pair
		if !analysis.Reset.ParsedTime.IsZero() && !hasNonChromeBelow(lines, reset, analysis.MenuVisible) && IsClaudeCode(pair) {
			analysis.Actionable = true
		}
	} else if analysis.IsLimited {
		analysis.Family = ClassifyFamily(content)
	}
	if analysis.Reset.ParsedTime.IsZero() {
		if match, ok := ClassifyTransient(strings.Join(lines, "\n")); ok {
			analysis.Transient = true
			analysis.TransientClass = match.Class
			// A non-reset transient is its own class, not an unparseable
			// usage-limit episode. This lets the coordinator route it to the
			// opt-in transient path without weakening reset-bearing precedence.
			analysis.IsLimited = false
			analysis.Actionable = false
		}
	}
	return analysis
}

// HasRateLimitMenu reports a clear wait/stop menu in the live tail. It does
// not select an option; jobs use this only to require manual validation.
func HasRateLimitMenu(content string) bool { return hasMenuLines(visibleTail(content)) }

func visibleTail(content string) []string {
	lines := terminal.Tail(maskQuoted(terminal.Lines(content)), 100)
	return trimTrailingChrome(lines)
}

func trimTrailingChrome(lines []string) []string {
	end := len(lines)
	for end > 0 && isChromeLine(lines[end-1]) {
		end--
	}
	return lines[:end]
}

func isChromeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || boxRuleLine.MatchString(trimmed) {
		return true
	}
	if strings.HasPrefix(trimmed, "│") && strings.HasSuffix(trimmed, "│") && strings.Contains(trimmed, ">") {
		return true
	}
	if trimmed == "❯" || trimmed == ">" {
		return true
	}
	if strings.HasPrefix(trimmed, "⏵⏵ ") || trimmed == "⏵⏵" {
		return true
	}
	if trimmed == "? for shortcuts" || regexp.MustCompile(`^\|\s+v\d+\.\d+\.\d+`).MatchString(trimmed) {
		return true
	}
	if regexp.MustCompile(`^\[[^]]+\|\s*Max\]`).MatchString(trimmed) {
		return true
	}
	if regexp.MustCompile(`^✓\s+(?:Bash|Read|Edit|Write|Grep|Glob|Task)\s+×\d+\b`).MatchString(trimmed) {
		return true
	}
	if regexp.MustCompile(`^[□◼✓]\s+(?:All\s+todos\s+complete|(?:Explore|Implement|Test|Review|Task|TODO)\b|.*\b(?:todo|todos)\b)`).MatchString(trimmed) {
		return true
	}
	if regexp.MustCompile(`^\d+\s+tasks\s*\(`).MatchString(trimmed) || regexp.MustCompile(`^\+\d+\s+completed\b`).MatchString(trimmed) {
		return true
	}
	if strings.HasPrefix(trimmed, "✻ ") || regexp.MustCompile(`^Opening your options…?$`).MatchString(trimmed) {
		return true
	}
	return false
}

func maskQuoted(lines []string) []string {
	masked := append([]string(nil), lines...)
	inFence := false
	inToolEcho := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			masked[i] = ""
			inFence = !inFence
			continue
		}
		if inFence || strings.HasPrefix(trimmed, "> ") {
			masked[i] = ""
			continue
		}
		if inToolEcho {
			if isToolChild(line) {
				masked[i] = ""
				continue
			}
			inToolEcho = false
		}
		if isToolHeader(trimmed) {
			masked[i] = ""
			inToolEcho = true
		}
	}
	return masked
}

func isToolHeader(line string) bool {
	return (strings.HasPrefix(line, "⏺ ") || strings.HasPrefix(line, "● ") || strings.HasPrefix(line, "∙ ") || strings.HasPrefix(line, "Name(")) && strings.Contains(line, "(")
}

func isToolChild(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "⎿") || strings.HasPrefix(trimmed, "└") || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
}

func hasMenuLines(lines []string) bool {
	hasTitle, hasOption, hasWaitOrCancel := false, false, false
	for _, line := range lines {
		trimmed := normalizeMenuLine(line)
		if strings.Contains(trimmed, "What do you want to do?") {
			hasTitle = true
		}
		if menuOptionLine.MatchString(trimmed) {
			hasOption = true
		}
		if strings.Contains(trimmed, "Stop and wait for limit to reset") || strings.Contains(trimmed, "Enter to confirm") || strings.Contains(trimmed, "Esc to cancel") {
			hasWaitOrCancel = true
		}
	}
	return hasTitle && hasOption && hasWaitOrCancel
}

func normalizeMenuLine(line string) string {
	trimmed := strings.TrimSpace(line)
	for _, border := range []string{"│", "┃"} {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, border))
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, border))
	}
	return trimmed
}

func isMenuContentLine(line string) bool {
	trimmed := normalizeMenuLine(line)
	return strings.Contains(trimmed, "What do you want to do?") ||
		menuOptionLine.MatchString(trimmed) ||
		strings.Contains(trimmed, "Enter to confirm") ||
		strings.Contains(trimmed, "Esc to cancel") ||
		strings.Contains(trimmed, "Stop and wait for limit to reset")
}

func isMenuBlockLine(line string) bool {
	return isChromeLine(line) || isChromeLine(normalizeMenuLine(line)) || isMenuContentLine(line)
}

func pairedEvidence(lines []string) (int, int, bool) {
	lastLimit, resetAt := -1, -1
	for i, line := range lines {
		if !isLimitLine(line) {
			continue
		}
		for j := i; j < len(lines) && j <= i+6; j++ {
			if resetMarkerLine.MatchString(lines[j]) {
				lastLimit, resetAt = i, j
			}
		}
	}
	if lastLimit < 0 || resetAt < 0 {
		return 0, 0, false
	}
	return lastLimit, resetAt, true
}

func isLimitLine(line string) bool {
	return isRateLimitSignal(line) || ClassifyFamily(line) != ""
}

func hasNonChromeBelow(lines []string, resetAt int, menu bool) bool {
	for _, line := range lines[resetAt+1:] {
		if isChromeLine(line) || strings.TrimSpace(line) == "" {
			continue
		}
		// A transient API failure is part of the same blocked error screen;
		// it must not make a parseable usage-limit banner non-actionable.
		if _, ok := ClassifyTransient(line); ok {
			continue
		}
		if menu && isMenuBlockLine(line) {
			continue
		}
		return true
	}
	return false
}
