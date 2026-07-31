package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
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
		"--state-file", "/tmp/state.json", "--margin", "2m", "--max-wait", "24h", "--verify-timeout", "30s",
	}, &stderr)
	if err != nil {
		t.Fatalf("parseRunFlags: %v", err)
	}
	if cfg.Runtime != "tmux" || !reflect.DeepEqual(cfg.Panes, []string{"w1:p1", "w2:p1"}) || cfg.Interval.String() != "250ms" || cfg.Lines != 17 || !cfg.DryRun || cfg.TestPattern != "needle" || cfg.HerdrBin != "/bin/herdr" || cfg.Socket != "/tmp/herdr.sock" || cfg.Session != "s1" || cfg.Workspace != "w1" || cfg.StateFile != "/tmp/state.json" || cfg.Margin != 2*time.Minute || cfg.MaxWait != 24*time.Hour || cfg.VerifyTimeout != 30*time.Second {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestResolveStatePathAutoOffAndExplicit(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cases := []struct {
		name    string
		runtime string
		value   string
		want    string
	}{
		{name: "herdr auto", runtime: "herdr", value: "auto", want: store.DefaultPath()},
		{name: "tmux auto", runtime: "tmux", value: "auto", want: "off"},
		{name: "explicit", runtime: "tmux", value: "/tmp/custom.json", want: "/tmp/custom.json"},
		{name: "off", runtime: "herdr", value: "off", want: "off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveStatePath(runConfig{Runtime: tc.runtime, StateFile: tc.value})
			if got != tc.want {
				t.Fatalf("resolveStatePath() = %q, want %q", got, tc.want)
			}
		})
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
