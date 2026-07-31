package provider

import (
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
)

type testProvider struct {
	name   string
	detect func(string) bool
}

func (p testProvider) Name() string                                  { return p.name }
func (p testProvider) DetectContent(content string) bool             { return p.detect(content) }
func (p testProvider) Analyze(string, time.Time) detection.Analysis  { return detection.Analysis{} }
func (p testProvider) SafeToResume(string, time.Time) (bool, string) { return false, "" }
func (p testProvider) ResumeAction() ResumeAction                    { return ResumeAction{} }
func (p testProvider) AllowPeriodicNudge() bool                      { return true }

func TestRegistryResolutionHonorsHintsAndUniqueContentMatches(t *testing.T) {
	claude := testProvider{name: "claude", detect: func(content string) bool { return content == "both" || content == "only claude" }}
	codex := testProvider{name: "codex", detect: func(content string) bool { return content == "both" }}
	registry := NewRegistry(claude, codex)

	tests := []struct {
		name    string
		hint    string
		content string
		want    string
	}{
		{name: "hint match", hint: "codex", content: "both", want: "codex"},
		{name: "unknown hint", hint: "gemini", content: "both"},
		{name: "no hint single", content: "only claude", want: "claude"},
		{name: "no hint both", content: "both"},
		{name: "disabled", hint: "codex", content: "both"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := tt.content
			if tt.name == "disabled" {
				got := NewRegistry(claude).Resolve(tt.hint, content)
				if got != nil {
					t.Fatalf("Resolve() = %s, want no provider", got.Name())
				}
				return
			}
			got := registry.Resolve(tt.hint, content)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("Resolve() = %s, want no provider", got.Name())
				}
				return
			}
			if got == nil || got.Name() != tt.want {
				t.Fatalf("Resolve() = %#v, want %s", got, tt.want)
			}
		})
	}
}
