package provider_test

import (
	"testing"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/codex"
)

func TestTransientTextDoesNotProveProviderIdentityOrResolveWithoutHint(t *testing.T) {
	for _, content := range []string{
		"API error 429: too many requests",
		"API error: 503 service unavailable",
		"The service is overloaded",
		"temporarily limiting requests",
		"api error: connection reset",
	} {
		t.Run(content, func(t *testing.T) {
			if !mustTransient(content) {
				t.Fatal("test fixture stopped matching the transient hypothesis table")
			}
			if detection.IsClaudeCode(content) {
				t.Fatalf("IsClaudeCode(%q) = true, want false", content)
			}
			if codex.IsCodex(content) {
				t.Fatalf("IsCodex(%q) = true, want false", content)
			}
			registry := provider.NewRegistry(claude.New(""), codex.New(""))
			if got := registry.Resolve("", content); got != nil {
				t.Fatalf("Resolve without hint = %s, want no provider", got.Name())
			}
		})
	}
}

func mustTransient(content string) bool {
	_, ok := detection.ClassifyTransient(content)
	return ok
}
