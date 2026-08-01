package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	herdradapter "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/herdr"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func TestParseRunFlagsRequiresExplicitPane(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // isolate from a real user config
	var stderr bytes.Buffer
	_, err := parseRunFlags([]string{"--runtime", "herdr"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "at least one --pane") {
		t.Fatalf("error = %v, want required pane error", err)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("usage output = %q", stderr.String())
	}
}

func TestParseRunFlagsTransportResolutionMatrix(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cases := []struct {
		name        string
		args        []string
		config      string
		want        string
		wantErr     string
		wantWarning string
	}{
		{name: "default herdr", args: []string{"--pane", "w1:p1"}, want: "socket"},
		{name: "default herdr session", args: []string{"--pane", "w1:p1", "--session", "s1"}, want: "cli", wantWarning: "--session"},
		{name: "default tmux", args: []string{"--pane", "w1:p1", "--runtime", "tmux"}, want: "cli", wantWarning: "--runtime"},
		{name: "default tmux session", args: []string{"--pane", "w1:p1", "--runtime", "tmux", "--session", "s1"}, want: "cli", wantWarning: "--runtime"},
		{name: "yaml herdr cli", config: "runtime:\n  type: herdr\n  transport: cli\n", args: []string{"--pane", "w1:p1"}, want: "cli"},
		{name: "yaml herdr socket", config: "runtime:\n  type: herdr\n  transport: socket\n", args: []string{"--pane", "w1:p1"}, want: "socket"},
		{name: "yaml herdr cli session", config: "runtime:\n  type: herdr\n  transport: cli\n", args: []string{"--pane", "w1:p1", "--session", "s1"}, want: "cli"},
		{name: "yaml tmux cli", config: "runtime:\n  type: tmux\n  transport: cli\n", args: []string{"--pane", "w1:p1"}, want: "cli"},
		{name: "yaml tmux socket", config: "runtime:\n  type: tmux\n  transport: socket\n", args: []string{"--pane", "w1:p1"}, wantErr: "runtime.transport socket requires runtime.type herdr"},
		{name: "flag herdr cli", args: []string{"--pane", "w1:p1", "--transport", "cli"}, want: "cli"},
		{name: "flag herdr socket", args: []string{"--pane", "w1:p1", "--transport", "socket"}, want: "socket"},
		{name: "flag socket session", args: []string{"--pane", "w1:p1", "--transport", "socket", "--session", "s1"}, wantErr: "--session is unsupported"},
		{name: "flag socket tmux", args: []string{"--pane", "w1:p1", "--transport", "socket", "--runtime", "tmux"}, wantErr: "requires --runtime herdr"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configPath := ""
			if tc.config != "" {
				configPath = filepath.Join(t.TempDir(), "config.yaml")
				if err := os.WriteFile(configPath, []byte("version: 1\n"+tc.config), 0600); err != nil {
					t.Fatal(err)
				}
				tc.args = append([]string{"--config", configPath}, tc.args...)
			}
			var stderr bytes.Buffer
			cfg, err := parseRunFlags(tc.args, &stderr)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, stderr=%q; want %q", err, stderr.String(), tc.wantErr)
				}
				return
			}
			if err != nil || cfg.Transport != tc.want {
				t.Fatalf("transport = %q, err = %v, stderr=%q; want %q", cfg.Transport, err, stderr.String(), tc.want)
			}
			if tc.wantWarning != "" && !strings.Contains(stderr.String(), tc.wantWarning) {
				t.Fatalf("stderr = %q, want warning naming %q", stderr.String(), tc.wantWarning)
			}
			if tc.wantWarning == "" && strings.Contains(stderr.String(), "warning:") {
				t.Fatalf("stderr = %q, want no fallback warning", stderr.String())
			}
		})
	}
}

func TestParseRunFlagsDefaultsToSocketTransport(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stderr bytes.Buffer
	cfg, err := parseRunFlags([]string{"--pane", "w1:p1"}, &stderr)
	if err != nil || cfg.Transport != "socket" {
		t.Fatalf("transport = %q, err = %v; want socket default", cfg.Transport, err)
	}
}

func TestParseRunFlagsWaitForPanesDefaultsOffAndHonorsFlagAndYAML(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stderr bytes.Buffer
	cfg, err := parseRunFlags([]string{"--pane", "w1:p1"}, &stderr)
	if err != nil {
		t.Fatalf("default parse: %v", err)
	}
	if cfg.WaitForPanes {
		t.Fatal("WaitForPanes = true by default, want false")
	}
	cfg, err = parseRunFlags([]string{"--pane", "w1:p1", "--wait-for-panes"}, &stderr)
	if err != nil || !cfg.WaitForPanes {
		t.Fatalf("flag parse cfg=%#v err=%v, want enabled", cfg, err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nmonitoring:\n  panes: [w1:p1]\n  wait_for_panes: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = parseRunFlags([]string{"--config", configPath}, &stderr)
	if err != nil || !cfg.WaitForPanes {
		t.Fatalf("YAML parse cfg=%#v err=%v, want enabled", cfg, err)
	}
	cfg, err = parseRunFlags([]string{"--config", configPath, "--wait-for-panes=false"}, &stderr)
	if err != nil || cfg.WaitForPanes {
		t.Fatalf("explicit false cfg=%#v err=%v, want disabled", cfg, err)
	}
}

func TestRunCommandWaitForPanesOffKeepsStartupExitContract(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HERDR_PANE_ID", "")
	for _, tc := range []struct {
		name string
		mode string
		want string
	}{
		{name: "list error", mode: "error", want: "Error: list panes:"},
		{name: "zero matches", mode: "empty", want: "Error: none of the requested panes were found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := filepath.Join(t.TempDir(), "herdr")
			body := "#!/bin/sh\n"
			if tc.mode == "error" {
				body += "echo 'list failed' >&2\nexit 1\n"
			} else {
				body += "echo '{\"result\":{\"panes\":[]}}'\n"
			}
			if err := os.WriteFile(script, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			got := runCommand([]string{"--transport", "cli", "--pane", "w1:p1", "--herdr-bin", script, "--state-file", "off"}, nil, &stderr)
			if got != 1 || !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("exit=%d stderr=%q, want exit 1 and %q", got, stderr.String(), tc.want)
			}
		})
	}
}

func TestListPanesStartupOffPreservesFailFastErrors(t *testing.T) {
	fake := &runtime.Fake{Errs: map[string]error{"ListPanes": errors.New("connection refused")}}
	_, err := listPanesStartup(context.Background(), fake, []string{"w1:p1"}, "", false, io.Discard)
	if err == nil || err.Error() != "list panes: connection refused" {
		t.Fatalf("error = %v, want byte-identical list error", err)
	}
	fake.Errs = nil
	fake.PanesList = nil
	_, err = listPanesStartup(context.Background(), fake, []string{"w1:p1"}, "", false, io.Discard)
	if err == nil || err.Error() != "none of the requested panes were found" {
		t.Fatalf("error = %v, want byte-identical empty-match error", err)
	}
}

func TestWaitForPanesRetriesReachabilityAndLogsStateChanges(t *testing.T) {
	fake := &runtime.Fake{
		PanesList: []runtime.Pane{{ID: "w1:p1"}},
		Errs:      map[string]error{"ListPanes": syscall.ECONNREFUSED},
	}
	var log bytes.Buffer
	waits := 0
	got, err := waitForPanes(context.Background(), fake, []string{"w1:p1"}, "", &log, func(ctx context.Context, _ time.Duration) error {
		waits++
		fake.Errs["ListPanes"] = nil
		return nil
	})
	if err != nil || !reflect.DeepEqual(got, fake.PanesList) {
		t.Fatalf("panes=%#v err=%v, want recovered pane", got, err)
	}
	if waits != 1 {
		t.Fatalf("wait calls = %d, want 1", waits)
	}
	if strings.Count(log.String(), "waiting for panes") != 1 || strings.Count(log.String(), "panes available") != 1 {
		t.Fatalf("log=%q, want one retry-state and one recovery message", log.String())
	}
}

func TestWaitForPanesRetriesAbsentSocketTimeoutEOFAndEmptyMatches(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{name: "connection refused", err: syscall.ECONNREFUSED},
		{name: "absent socket", err: os.ErrNotExist},
		{name: "timeout", err: context.DeadlineExceeded},
		{name: "mid-request eof", err: io.ErrUnexpectedEOF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &runtime.Fake{PanesList: []runtime.Pane{{ID: "w1:p1"}}, Errs: map[string]error{"ListPanes": tc.err}}
			calls := 0
			got, err := waitForPanes(context.Background(), fake, []string{"w1:p1"}, "", io.Discard, func(context.Context, time.Duration) error {
				calls++
				fake.Errs["ListPanes"] = nil
				return nil
			})
			if err != nil || len(got) != 1 || calls != 1 {
				t.Fatalf("panes=%#v err=%v waits=%d, want one retry and recovery", got, err, calls)
			}
		})
	}

	fake := &runtime.Fake{}
	waits := 0
	got, err := waitForPanes(context.Background(), fake, []string{"w1:p1"}, "", io.Discard, func(context.Context, time.Duration) error {
		waits++
		fake.PanesList = []runtime.Pane{{ID: "w1:p1"}}
		return nil
	})
	if err != nil || len(got) != 1 || waits != 1 {
		t.Fatalf("empty-match recovery panes=%#v err=%v waits=%d", got, err, waits)
	}
}

func TestWaitForPanesDoesNotRetryPermanentFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{name: "protocol mismatch", err: herdradapter.HerdrError{Code: "protocol_mismatch", Message: "unsupported protocol"}},
		{name: "malformed response", err: errors.New("decode herdr response: invalid character")},
		{name: "permission denial", err: os.ErrPermission},
		{name: "configuration error", err: errors.New("socket path is not absolute")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &runtime.Fake{Errs: map[string]error{"ListPanes": tc.err}}
			waits := 0
			_, err := waitForPanes(context.Background(), fake, []string{"w1:p1"}, "", io.Discard, func(context.Context, time.Duration) error {
				waits++
				return nil
			})
			if err == nil || !errors.Is(err, tc.err) || waits != 0 {
				t.Fatalf("err=%v waits=%d, want permanent error and no retry", err, waits)
			}
		})
	}
}

func TestWaitForPanesStopsWhenContextIsCancelled(t *testing.T) {
	fake := &runtime.Fake{Errs: map[string]error{"ListPanes": syscall.ECONNREFUSED}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := waitForPanes(ctx, fake, []string{"w1:p1"}, "", io.Discard, func(ctx context.Context, _ time.Duration) error {
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestRuntimeForRunRejectsSyntacticallyInvalidSocketPaths(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "empty", path: "", want: "socket path is empty"},
		{name: "relative", path: "herdr.sock", want: "not absolute"},
		{name: "too long", path: "/tmp/" + strings.Repeat("x", 110), want: "unix limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runtimeForRun(runConfig{Runtime: "herdr", Transport: "socket", Socket: tc.path, SocketExplicit: true})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseRunFlagsAbsentConfigPreservesExistingDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var firstErr, secondErr bytes.Buffer
	want, err := parseRunFlags([]string{"--pane", "w1:p1"}, &firstErr)
	if err != nil {
		t.Fatalf("baseline parseRunFlags: %v", err)
	}
	got, err := parseRunFlags([]string{"--pane", "w1:p1"}, &secondErr)
	if err != nil {
		t.Fatalf("default-path parity parseRunFlags: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configless parse = %#v, baseline = %#v", got, want)
	}
}

func TestParseRunFlagsConfigPrecedenceAndPanes(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(`version: 1
runtime:
  transport: socket
monitoring:
  panes: [configured:p1]
  interval: 7s
  lines: 99
resume:
  margin: 2m
providers:
  enabled: [claude]
state:
  file: /tmp/config-state.json
`), 0600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cfg, err := parseRunFlags([]string{"--config", configPath, "--interval", "5s", "--dry-run=false", "--lines", "10"}, &stderr)
	if err != nil {
		t.Fatalf("parseRunFlags: %v\nstderr=%s", err, stderr.String())
	}
	if cfg.Transport != "socket" || !reflect.DeepEqual(cfg.Panes, []string{"configured:p1"}) || cfg.Interval != 5*time.Second || cfg.Lines != 10 || cfg.DryRun || cfg.Margin != 2*time.Minute || cfg.Providers != "claude" || cfg.StateFile != "/tmp/config-state.json" {
		t.Fatalf("merged config = %#v", cfg)
	}
}

func TestParseRunFlagsExplicitMissingConfigErrors(t *testing.T) {
	var stderr bytes.Buffer
	_, err := parseRunFlags([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml"), "--pane", "w1:p1"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "config") {
		t.Fatalf("error = %v, stderr=%q; want missing config error", err, stderr.String())
	}
}

func TestParseRunFlagsValidatesSocketTransportSessionAndRuntime(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown transport", args: []string{"--pane", "p1", "--transport", "bogus"}, want: "unsupported transport"},
		{name: "socket session", args: []string{"--pane", "p1", "--transport", "socket", "--session", "s1"}, want: "--session is unsupported"},
		{name: "socket tmux", args: []string{"--runtime", "tmux", "--pane", "p1", "--transport", "socket"}, want: "requires --runtime herdr"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, err := parseRunFlags(tc.args, &stderr)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, stderr = %q; want %q", err, stderr.String(), tc.want)
			}
		})
	}
}

func TestRuntimeForRunSelectsCLIByDefaultAndSocketExplicitly(t *testing.T) {
	cli, err := runtimeForRun(runConfig{Runtime: "herdr", Transport: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cli.(*herdradapter.Adapter); !ok {
		t.Fatalf("default runtime = %T, want CLI adapter", cli)
	}
	socket, err := runtimeForRun(runConfig{Runtime: "herdr", Transport: "socket", Socket: "/tmp/fake-herdr.sock"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := socket.(*herdradapter.Socket); !ok {
		t.Fatalf("socket runtime = %T, want socket adapter", socket)
	}
}

func TestHerdrDetectionMatchRegexCoversBothProviderAnchors(t *testing.T) {
	for _, sample := range []string{"limit reached", "You've hit your usage limit", "out of credits", "try again in 5 minutes"} {
		matched, err := regexp.MatchString(herdrDetectionMatchRegex, sample)
		if err != nil || !matched {
			t.Fatalf("regex did not match %q: matched=%v err=%v", sample, matched, err)
		}
	}
}

func TestDetectionEventPumpCoalescesBurstWithoutTicker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticker := make(chan time.Time)
	events := make(chan runtime.Event, 32)
	ticks := startDetectionPump(ctx, ticker, events)
	for i := 0; i < 20; i++ {
		events <- runtime.Event{Kind: runtime.EventOutputMatched, PaneID: "p1"}
	}
	select {
	case <-ticks:
	case <-time.After(time.Second):
		t.Fatal("event burst did not produce a detection tick")
	}
	select {
	case <-ticks:
		t.Fatal("event burst produced more than one immediate detection tick")
	case <-time.After(400 * time.Millisecond):
	}
}

func TestDetectionEventPumpForwardsTickerAndIgnoresNonTriggers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticker := make(chan time.Time, 1)
	events := make(chan runtime.Event, 1)
	ticks := startDetectionPump(ctx, ticker, events)
	events <- runtime.Event{Kind: runtime.EventPanesChanged}
	want := time.Unix(9, 0)
	ticker <- want
	select {
	case got := <-ticks:
		if !got.Equal(want) {
			t.Fatalf("tick = %v, want %v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("ticker tick was not forwarded")
	}
}

func TestRecycleDueRequiresTriggerAndSixtySecondBound(t *testing.T) {
	base := time.Unix(100, 0)
	if recycleDue(base, time.Time{}, base.Add(2*time.Minute)) {
		t.Fatal("recycleDue() = true without a trigger")
	}
	if recycleDue(base, base.Add(time.Second), base.Add(59*time.Second)) {
		t.Fatal("recycleDue() = true before the sixty-second bound")
	}
	if !recycleDue(base, base.Add(time.Second), base.Add(time.Minute)) {
		t.Fatal("recycleDue() = false after a triggered sixty-second interval")
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

func TestParseRunFlagsRejectsVerificationTimeoutBelowTwiceInterval(t *testing.T) {
	var stderr bytes.Buffer
	_, err := parseRunFlags([]string{"--pane", "w1:p1", "--interval", "10s", "--verify-timeout", "19s"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "at least twice") {
		t.Fatalf("error = %v, want twice-interval rejection", err)
	}
}

func TestParseRunFlagsPreservesExplicitZeroMargin(t *testing.T) {
	var stderr bytes.Buffer
	cfg, err := parseRunFlags([]string{"--pane", "w1:p1", "--margin", "0"}, &stderr)
	if err != nil || cfg.Margin != 0 {
		t.Fatalf("cfg=%#v err=%v, want explicit zero margin", cfg, err)
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

func TestRunCommandFailsOnHeldRunLockBeforeRuntimeConstruction(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	lock, err := store.AcquireRunLock(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	var stderr bytes.Buffer
	if got := runCommand([]string{"--pane", "w1:p1", "--state-file", statePath, "--herdr-bin", "/missing/herdr"}, nil, &stderr); got != 1 {
		t.Fatalf("runCommand exit = %d, want 1; stderr=%q", got, stderr.String())
	}
	if !strings.Contains(stderr.String(), "already in use") || !strings.Contains(stderr.String(), filepath.Clean(statePath)) || !strings.Contains(stderr.String(), strconv.Itoa(os.Getpid())) {
		t.Fatalf("stderr = %q, want run-lock error", stderr.String())
	}
	if strings.Contains(stderr.String(), "get own pane") {
		t.Fatalf("runtime was constructed after lock failure: %q", stderr.String())
	}
}

func TestRunCommandStateFileOffDoesNotCreateRunLock(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	var stderr bytes.Buffer
	if got := runCommand([]string{"--pane", "w1:p1", "--state-file", "off", "--herdr-bin", "/missing/herdr"}, nil, &stderr); got != 1 {
		t.Fatalf("runCommand exit = %d, want runtime failure 1; stderr=%q", got, stderr.String())
	}
	if _, err := os.Stat(statePath + ".run"); !os.IsNotExist(err) {
		t.Fatalf("off state created run lock: err=%v", err)
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

func TestFilterPanesByIdentityKeepsMovedMonitoredTerminal(t *testing.T) {
	panes := []runtime.Pane{{ID: "w2:p9", TerminalID: "term-1"}, {ID: "w3:p1", TerminalID: "other"}}
	got, excluded := filterPanesByIdentity(panes, []string{"w1:p1"}, map[string]struct{}{"term-1": {}}, "")
	if excluded || !reflect.DeepEqual(got, []runtime.Pane{{ID: "w2:p9", TerminalID: "term-1"}}) {
		t.Fatalf("filtered moved panes = %#v, excluded=%v", got, excluded)
	}
}
