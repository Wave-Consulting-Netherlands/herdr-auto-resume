package detection

import (
	"regexp"
	"strings"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/terminal"
)

// Claude Code UI patterns - multiple approaches for robustness
var (
	// Box-drawing characters used in Claude Code's UI
	boxDrawingPattern = regexp.MustCompile(`[─│┌┐└┘├┤┬┴┼╭╮╯╰]`)

	// The input prompt pattern: > at start of line (with possible ANSI codes)
	promptPattern = regexp.MustCompile(`(?m)^(\x1b\[[0-9;]*m)*>\s`)

	// Claude Code status bar patterns (model names, etc.)
	statusBarPattern = regexp.MustCompile(`(?i)(claude|anthropic|sonnet|opus|haiku)`)

	// Footer hint that appears in both prompt and menu modes
	footerHintPattern = regexp.MustCompile(`ctrl-g to edit`)

	// Menu selector used in question/choice UI
	menuSelectorPattern = regexp.MustCompile(`❯`)

	// Dashed separator line used in Claude Code UI
	dashedSeparator = regexp.MustCompile(`╌{10,}`)
)

// IsClaudeCode detects if pane content appears to be running Claude Code
func IsClaudeCode(content string) bool {
	// Rate limit message is definitive - if we see it, it's Claude Code
	if isRateLimitSignal(content) {
		return true
	}

	// Footer hint is very reliable
	if footerHintPattern.MatchString(content) {
		return true
	}

	// Must have box-drawing characters for other detection methods
	if !boxDrawingPattern.MatchString(content) {
		return false
	}

	// Any of these patterns indicate Claude Code
	if promptPattern.MatchString(content) {
		return true
	}
	if statusBarPattern.MatchString(content) {
		return true
	}
	if menuSelectorPattern.MatchString(content) {
		return true
	}
	if dashedSeparator.MatchString(content) {
		return true
	}

	return false
}

// IsIdlePrompt reports whether the recent tail ends at Claude Code's plain input
// prompt rather than a menu or another visible state.
func IsIdlePrompt(content string) bool {
	lines := GetVisibleLines(content)
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	for _, line := range lines {
		if strings.Contains(line, "❯") {
			return false
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], ">") {
			return true
		}
	}
	return false
}

// StripANSI removes ANSI escape codes from a string
func StripANSI(s string) string {
	return terminal.StripANSI(s)
}

// GetVisibleLines returns non-empty lines from content
func GetVisibleLines(content string) []string {
	lines := strings.Split(content, "\n")
	visible := make([]string, 0, len(lines))
	for _, line := range lines {
		stripped := strings.TrimSpace(StripANSI(line))
		if stripped != "" {
			visible = append(visible, stripped)
		}
	}
	return visible
}
