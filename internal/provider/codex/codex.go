// Package codex detects and resumes the Codex CLI's live terminal UI.
package codex

import (
	"regexp"
	"strings"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/terminal"
)

const defaultPrompt = "Continue the previous task from where you stopped. First inspect the current repository state and existing progress before making further changes."

const (
	FamilyUsageLimit      detection.Family = "usage-limit"
	FamilyModelUsageLimit detection.Family = "model-usage-limit"
	FamilyCredits         detection.Family = "credits"
	FamilySpendCap        detection.Family = "spend-cap"
)

var (
	usageLimitBanner = regexp.MustCompile(`(?i)^\s*■\s*You've hit your usage limit(?:\b|$)`)
	creditsBanner    = regexp.MustCompile(`(?i)^\s*■\s*Your workspace is out of credits\.\s*Add credits to continue\.$`)
	spendCapBanner   = regexp.MustCompile(`(?i)^\s*■\s*You hit your spend cap set in your workspace\.\s*Increase your spend cap to continue\.$`)
	codexComposer    = regexp.MustCompile(`(?m)^\s*›(?:\s|$)`)
	codexFooter      = regexp.MustCompile(`(?im)^\s*gpt-[^\n]*·[^\n]*(?:context|weekly)\s+\d+%\s+left`)
	codexWorking     = regexp.MustCompile(`(?m)^\s*•\s+Working\s+\([^\n]*\besc to interrupt\)`)
	codexWorked      = regexp.MustCompile(`(?m)^\s*─\s+Worked for\s+\d+[^\n]*─`)
)

type Codex struct {
	prompt string
}

func New(prompt string) *Codex {
	if prompt == "" {
		prompt = defaultPrompt
	}
	return &Codex{prompt: prompt}
}

func (c *Codex) Name() string { return "codex" }

// IsCodex identifies the Codex-specific composer, footer, work chrome, or
// red transcript banner. All chrome checks are line-anchored to avoid prose
// mentioning the glyphs from becoming provider identity.
func IsCodex(content string) bool {
	normalized := terminal.StripANSI(content)
	if findBanner(normalized) != "" {
		return true
	}
	return codexComposer.MatchString(normalized) || codexFooter.MatchString(normalized) ||
		codexWorking.MatchString(normalized) || codexWorked.MatchString(normalized)
}

func (c *Codex) DetectContent(content string) bool { return IsCodex(content) }

func (c *Codex) Analyze(content string, now time.Time) detection.Analysis {
	return analyzeCore(content, now)
}

func analyzeCore(content string, now time.Time) detection.Analysis {
	line := findBanner(content)
	if line == "" {
		return detection.Analysis{}
	}
	family := classifyBanner(line)
	spec := parseCodexReset(line, now)
	return detection.Analysis{
		IsLimited:   true,
		Actionable:  !spec.ParsedTime.IsZero(),
		MenuVisible: false,
		Family:      family,
		Reset:       spec,
		Evidence:    line,
	}
}

func findBanner(content string) string {
	var found string
	for _, line := range terminal.Lines(content) {
		line = strings.TrimSpace(line)
		if usageLimitBanner.MatchString(line) || creditsBanner.MatchString(line) || spendCapBanner.MatchString(line) {
			found = line
		}
	}
	return found
}

func classifyBanner(line string) detection.Family {
	switch {
	case creditsBanner.MatchString(line):
		return FamilyCredits
	case spendCapBanner.MatchString(line):
		return FamilySpendCap
	case strings.Contains(strings.ToLower(line), "usage limit for"):
		return FamilyModelUsageLimit
	case usageLimitBanner.MatchString(line):
		return FamilyUsageLimit
	default:
		return ""
	}
}

func (c *Codex) SafeToResume(content string, now time.Time) (bool, string) {
	if !IsCodex(content) {
		return false, "pane is not Codex"
	}
	analysis := c.Analyze(content, now)
	idle := codexComposer.MatchString(terminal.StripANSI(content))
	if analysis.IsLimited {
		if !analysis.Actionable {
			return false, "terminal is not in a safe blocked or idle state"
		}
		return true, "validation passed"
	}
	if !idle {
		return false, "terminal is not in a safe blocked or idle state"
	}
	return true, "validation passed"
}

func (c *Codex) ResumeAction() provider.ResumeAction {
	return provider.ResumeAction{Text: c.prompt, SubmitKey: "enter"}
}

func (c *Codex) AllowPeriodicNudge() bool { return false }

var _ provider.Provider = (*Codex)(nil)
