package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/walt-verweij/herdr-auto-resume/internal/runtime"
)

func TestParseRunFlagsRequiresExplicitPane(t *testing.T) {
	var stderr bytes.Buffer
	_, err := parseRunFlags([]string{"--runtime", "herdr"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "at least one --pane") {
		t.Fatalf("error = %v, want required pane error", err)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("usage output = %q", stderr.String())
	}
}

func TestParseRunFlagsAcceptsRepeatedPanesAndOptions(t *testing.T) {
	var stderr bytes.Buffer
	cfg, err := parseRunFlags([]string{
		"--runtime", "tmux", "--pane", "w1:p1", "--pane", "w2:p1",
		"--interval", "250ms", "--lines", "17", "--dry-run", "--test-pattern", "needle",
		"--herdr-bin", "/bin/herdr", "--socket", "/tmp/herdr.sock", "--session", "s1", "--workspace", "w1",
	}, &stderr)
	if err != nil {
		t.Fatalf("parseRunFlags: %v", err)
	}
	if cfg.Runtime != "tmux" || !reflect.DeepEqual(cfg.Panes, []string{"w1:p1", "w2:p1"}) || cfg.Interval.String() != "250ms" || cfg.Lines != 17 || !cfg.DryRun || cfg.TestPattern != "needle" || cfg.HerdrBin != "/bin/herdr" || cfg.Socket != "/tmp/herdr.sock" || cfg.Session != "s1" || cfg.Workspace != "w1" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestFilterPanesIsStrictAndExcludesSelf(t *testing.T) {
	panes := []runtime.Pane{{ID: "w1:p1"}, {ID: "w2:p1"}, {ID: "w3:p1"}}
	got, excluded := filterPanes(panes, []string{"w3:p1", "w1:p1", "missing"}, "w1:p1")
	if !excluded {
		t.Fatal("excluded = false, want true")
	}
	want := []runtime.Pane{{ID: "w3:p1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered panes = %#v, want %#v", got, want)
	}
}
