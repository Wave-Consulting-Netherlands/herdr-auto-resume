package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunCLIVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := runCLI([]string{"version"}, &stdout, &stderr); got != 0 {
		t.Fatalf("runCLI exit = %d, want 0", got)
	}
	if stdout.String() != version+"\n" {
		t.Fatalf("version output = %q, want %q", stdout.String(), version+"\n")
	}
	if stderr.Len() != 0 {
		t.Fatalf("version stderr = %q, want empty", stderr.String())
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

func TestRunCLIStubSubcommands(t *testing.T) {
	for _, subcommand := range []string{"run", "doctor"} {
		t.Run(subcommand, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := runCLI([]string{subcommand}, &stdout, &stderr); got != 2 {
				t.Fatalf("runCLI exit = %d, want 2", got)
			}
			if !strings.Contains(stderr.String(), "not implemented") {
				t.Fatalf("stderr = %q, want not implemented", stderr.String())
			}
		})
	}
}
