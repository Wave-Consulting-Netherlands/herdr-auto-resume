// Package provider contains provider-neutral detection and resume contracts.
package provider

import (
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
)

// ResumeAction is the allow-listed input sequence for a provider's live UI.
type ResumeAction struct {
	KeysBefore []string
	Text       string
	SubmitKey  string
}

// Provider owns provider-specific pane classification, analysis, safety, and
// resume input. It intentionally has no runtime or persistence dependency.
type Provider interface {
	Name() string
	DetectContent(content string) bool
	Analyze(content string, now time.Time) detection.Analysis
	SafeToResume(content string, now time.Time) (bool, string)
	ResumeAction() ResumeAction
	AllowPeriodicNudge() bool
}

// TransientRetrySafety is deliberately separate from Provider.SafeToResume.
// The coordinator/job layer invokes it only for an explicitly enabled,
// identity-established transient retry; ordinary resume callers cannot gain
// new authorization through this predicate.
type TransientRetrySafety interface {
	SafeToRetryTransient(content string, now time.Time) (bool, string)
}
