package main

import (
	"bytes"
	"runtime"
	"strings"
	"testing"
)

func TestRunCLIVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := runCLI([]string{"version"}, &stdout, &stderr); got != 0 {
		t.Fatalf("runCLI exit = %d, want 0", got)
	}
	if !strings.HasPrefix(stdout.String(), "herdr-auto-resume ") || !strings.Contains(stdout.String(), "(commit ") || !strings.Contains(stdout.String(), ", built ") || !strings.Contains(stdout.String(), ", go") {
		t.Fatalf("version output = %q, want packaged identity and provenance", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("version stderr = %q, want empty", stderr.String())
	}
}

func TestVersionOutputUsesInjectedBuildValues(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	t.Cleanup(func() { version, commit, date = oldVersion, oldCommit, oldDate })
	version, commit, date = "v0.2.0", "abc1234", "2026-07-31T12:00:00Z"

	var stdout, stderr bytes.Buffer
	if got := runCLI([]string{"version"}, &stdout, &stderr); got != 0 {
		t.Fatalf("runCLI exit = %d, want 0", got)
	}
	want := "herdr-auto-resume v0.2.0 (commit abc1234, built 2026-07-31T12:00:00Z, " + runtime.Version() + ")\n"
	if stdout.String() != want {
		t.Fatalf("version output = %q, want %q", stdout.String(), want)
	}
}

func TestVersionDevFallbackDoesNotPanic(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	t.Cleanup(func() { version, commit, date = oldVersion, oldCommit, oldDate })
	version, commit, date = "dev", "none", "unknown"

	if got := versionOutput(); !strings.HasPrefix(got, "herdr-auto-resume dev (commit ") {
		t.Fatalf("version output = %q, want dev identity", got)
	}
}

func TestRunCLIUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := runCLI([]string{"unknown"}, &stdout, &stderr); got != 2 {
		t.Fatalf("runCLI exit = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("stderr = %q, want unknown subcommand", stderr.String())
	}
}

func TestRunCLIRequiresPane(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // isolate from a real user config
	var stdout, stderr bytes.Buffer
	if got := runCLI([]string{"run"}, &stdout, &stderr); got != 2 {
		t.Fatalf("runCLI exit = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), "at least one --pane") {
		t.Fatalf("stderr = %q, want required pane error", stderr.String())
	}
}
