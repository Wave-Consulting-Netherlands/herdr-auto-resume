package jobs

import (
	"regexp"
	"strings"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/terminal"
)

var stopAndWaitOption = regexp.MustCompile(`^❯\s*\d+\.\s+.*Stop and wait for limit to reset\b`)
var numberedMenuOption = regexp.MustCompile(`^❯?\s*\d+\.\s+`)

// safeStopAndWaitMenu requires the question and the cursor on the safe option
// in the same fresh pane read. It intentionally does not assign meaning to
// any option index; paid options may occupy any other index.
func safeStopAndWaitMenu(content string) bool {
	question := false
	stopAndWait := false
	for _, raw := range strings.Split(terminal.StripANSI(content), "\n") {
		line := strings.TrimSpace(strings.Trim(raw, "│┃|"))
		if line == "What do you want to do?" {
			question = true
		}
		if stopAndWaitOption.MatchString(line) {
			stopAndWait = true
		}
	}
	return question && stopAndWait
}

// looksLikeLimitMenu is intentionally broader than the strict action guard.
// When answering is enabled, any plausible interactive limit menu must stay
// manual if its question, option text, or cursor is malformed.
func looksLikeLimitMenu(content string) bool {
	question, numberedOption, stopText := false, false, false
	for _, raw := range strings.Split(terminal.StripANSI(content), "\n") {
		line := strings.TrimSpace(strings.Trim(raw, "│┃|"))
		if line == "What do you want to do?" {
			question = true
		}
		if numberedMenuOption.MatchString(line) {
			numberedOption = true
		}
		if strings.Contains(line, "Stop and wait for limit to reset") {
			stopText = true
		}
	}
	return (question && numberedOption) || (stopText && numberedOption)
}
