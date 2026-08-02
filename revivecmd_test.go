package main

import (
	"io"
	"strings"
	"testing"
)

func TestParseReviveFlagsRejectsTmux(t *testing.T) {
	_, _, err := parseReviveFlags([]string{"session", "--runtime", "tmux"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "runtime.type herdr") {
		t.Fatalf("expected tmux rejection naming runtime.type, got %v", err)
	}
}

func TestReviveCommandRejectsStateOff(t *testing.T) {
	var stderr strings.Builder
	code := reviveCommand([]string{"revive", "session", "--state-file", "off"}, io.Discard, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "persistent --state-file") {
		t.Fatalf("expected state-off rejection, code=%d stderr=%q", code, stderr.String())
	}
}

func TestSplitReviveArgsAcceptsFlagsAfterPrefix(t *testing.T) {
	positionals, flags := splitReviveArgs([]string{"session", "--state-file", "/tmp/state", "--runtime=herdr"})
	if len(positionals) != 1 || positionals[0] != "session" {
		t.Fatalf("positionals = %#v", positionals)
	}
	if strings.Join(flags, " ") != "--state-file /tmp/state --runtime=herdr" {
		t.Fatalf("flags = %#v", flags)
	}
}
