package codex

import (
	"regexp"
	"strings"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/terminal"
)

var (
	streamErrorPattern = regexp.MustCompile(`(?i)\bstream\s+error\b`)
	retryingPattern    = regexp.MustCompile(`(?i)\bretrying\s+\d+/\d+\b`)
	moreLinesPattern   = regexp.MustCompile(`^…\s+\+\d+\s+lines\s+\(ctrl\s+\+\s+t\s+to\s+view\s+transcript\)$`)
	composerLine       = regexp.MustCompile(`^›(?:\s|$)`)
)

func analyzeLive(content string, now time.Time) detection.Analysis {
	identityLines := liveIdentityLines(content)
	lines := trimTrailingChrome(identityLines)
	bannerIndex := findBannerIndex(lines)
	if bannerIndex < 0 {
		return detection.Analysis{}
	}
	banner, bannerEnd := joinWrappedBanner(lines, bannerIndex)
	analysis := analyzeCore(banner, now)
	if hasBusyLines(identityLines) || hasNonChromeBelow(lines, bannerEnd) {
		analysis.Actionable = false
	}
	return analysis
}

func liveIdentityLines(content string) []string {
	return terminal.Tail(maskQuoted(terminal.Lines(content)), 100)
}

func liveTailLines(content string) []string {
	return trimTrailingChrome(liveIdentityLines(content))
}

func maskQuoted(lines []string) []string {
	masked := append([]string(nil), lines...)
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			masked[i] = ""
			inFence = !inFence
			continue
		}
		if inFence || strings.HasPrefix(trimmed, "> ") {
			masked[i] = ""
		}
	}
	return masked
}

func trimTrailingChrome(lines []string) []string {
	end := len(lines)
	for end > 0 && isCodexChromeLine(lines[end-1]) {
		end--
	}
	return lines[:end]
}

func isCodexChromeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || composerLine.MatchString(trimmed) || codexFooter.MatchString(trimmed) || codexWorked.MatchString(trimmed) || codexWorking.MatchString(trimmed) {
		return true
	}
	if strings.HasPrefix(trimmed, "• ") || strings.HasPrefix(trimmed, "└") || moreLinesPattern.MatchString(trimmed) {
		return true
	}
	return false
}

func findBannerIndex(lines []string) int {
	found := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if usageLimitBanner.MatchString(trimmed) || creditsBanner.MatchString(trimmed) || spendCapBanner.MatchString(trimmed) {
			found = i
		}
	}
	return found
}

func joinWrappedBanner(lines []string, bannerIndex int) (string, int) {
	parts := []string{lines[bannerIndex]}
	end := bannerIndex + 1
	if !usageLimitBanner.MatchString(strings.TrimSpace(lines[bannerIndex])) {
		return strings.TrimSpace(lines[bannerIndex]), end
	}
	for end < len(lines) && end <= bannerIndex+3 {
		line := strings.TrimSpace(lines[end])
		if line == "" || strings.HasPrefix(line, "■ ") || composerLine.MatchString(line) || strings.HasPrefix(line, "─") || isCodexChromeLine(line) {
			break
		}
		parts = append(parts, line)
		end++
	}
	return collapseWhitespace(parts), end
}

func collapseWhitespace(parts []string) string {
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func hasBusyLiveTail(content string) bool { return hasBusyLines(liveIdentityLines(content)) }

func hasBusyLines(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), "esc to interrupt") || retryingPattern.MatchString(line) || streamErrorPattern.MatchString(line) {
			return true
		}
	}
	return false
}

func hasNonChromeBelow(lines []string, start int) bool {
	for _, line := range lines[start:] {
		if !isCodexChromeLine(line) {
			return true
		}
	}
	return false
}
