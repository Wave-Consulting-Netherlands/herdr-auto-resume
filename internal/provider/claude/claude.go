// Package claude adapts the established Claude detection behavior to the
// provider contract without moving or changing the detection implementation.
package claude

import (
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
)

const defaultPrompt = "continue"

type Claude struct {
	prompt string
}

func New(prompt string) *Claude {
	if prompt == "" {
		prompt = defaultPrompt
	}
	return &Claude{prompt: prompt}
}

func (c *Claude) Name() string { return "claude" }

func (c *Claude) DetectContent(content string) bool {
	return detection.IsClaudeCode(content)
}

func (c *Claude) Analyze(content string, now time.Time) detection.Analysis {
	return detection.Analyze(content, now)
}

// SafeToResume is the existing validation gate body, moved without semantic
// changes so jobs can apply the provider-specific gate through the interface.
func (c *Claude) SafeToResume(content string, now time.Time) (bool, string) {
	if !detection.IsClaudeCode(content) {
		return false, "pane is not Claude Code"
	}
	status := detection.CheckRateLimitAt(content, now)
	analysis := detection.Analyze(content, now)
	idle := detection.IsIdlePrompt(content)
	if analysis.MenuVisible || (!status.IsLimited && !idle) {
		return false, "terminal is not in a safe blocked or idle state"
	}
	return true, "validation passed"
}

func (c *Claude) ResumeAction() provider.ResumeAction {
	return provider.ResumeAction{KeysBefore: []string{"escape"}, Text: c.prompt, SubmitKey: "enter"}
}

func (c *Claude) AllowPeriodicNudge() bool { return true }

var _ provider.Provider = (*Claude)(nil)
