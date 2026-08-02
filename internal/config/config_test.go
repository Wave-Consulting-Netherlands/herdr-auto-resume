package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadParsesFullConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := writeConfig(t, `version: 1
runtime:
  type: herdr
  transport: socket
  herdr_bin: /bin/herdr
  socket: ~/herdr.sock
  workspace: team
monitoring:
  panes: [w1:p1, w2:p1]
  interval: 5s
  lines: 100
resume:
  margin: 2m
  max_wait: 24h
  verify_timeout: 30s
providers:
  enabled: [claude]
  claude_prompt: continue now
  codex_prompt: resume Codex
state:
  file: ~/state.json
`)

	got, found, err := Load(path)
	if err != nil || !found {
		t.Fatalf("Load() = %#v, found=%v, err=%v", got, found, err)
	}
	if got.Version != 1 || got.Runtime.Type != "herdr" || got.Runtime.Transport != "socket" || got.Runtime.HerdrBin != "/bin/herdr" || got.Runtime.Socket != filepath.Join(home, "herdr.sock") || got.Runtime.Workspace != "team" {
		t.Fatalf("runtime config = %#v", got.Runtime)
	}
	if len(got.Monitoring.Panes) != 2 || got.Monitoring.Interval != 5*time.Second || got.Monitoring.Lines != 100 {
		t.Fatalf("monitoring config = %#v", got.Monitoring)
	}
	if got.Resume.Margin != 2*time.Minute || got.Resume.MaxWait != 24*time.Hour || got.Resume.VerifyTimeout != 30*time.Second {
		t.Fatalf("resume config = %#v", got.Resume)
	}
	if strings.Join(got.Providers.Enabled, ",") != "claude" || got.Providers.ClaudePrompt != "continue now" || got.Providers.CodexPrompt != "resume Codex" {
		t.Fatalf("provider config = %#v", got.Providers)
	}
	if got.State.File != filepath.Join(home, "state.json") {
		t.Fatalf("state config = %#v", got.State)
	}
}

func TestLoadParsesWaitForPanes(t *testing.T) {
	path := writeConfig(t, "version: 1\nmonitoring:\n  wait_for_panes: true\n")
	cfg, found, err := Load(path)
	if err != nil || !found || !cfg.Has("monitoring.wait_for_panes") || !cfg.Monitoring.WaitForPanes {
		t.Fatalf("cfg=%#v found=%v err=%v, want wait_for_panes=true", cfg, found, err)
	}
}

func TestLoadParsesSessionFileChannel(t *testing.T) {
	path := writeConfig(t, "version: 1\nproviders:\n  session_file_channel: true\n")
	cfg, found, err := Load(path)
	if err != nil || !found || !cfg.Has("providers.session_file_channel") || !cfg.Providers.SessionFileChannel {
		t.Fatalf("cfg=%#v found=%v err=%v, want session file channel enabled", cfg, found, err)
	}
}

func TestLoadParsesAdmissionAndRequiresSessionFileChannel(t *testing.T) {
	path := writeConfig(t, "version: 1\nproviders:\n  session_file_channel: true\nmonitoring:\n  admit_session_matches: true\n")
	cfg, found, err := Load(path)
	if err != nil || !found || !cfg.Has("monitoring.admit_session_matches") || !cfg.Monitoring.AdmitSessionMatches {
		t.Fatalf("cfg=%#v found=%v err=%v, want admission enabled", cfg, found, err)
	}

	path = writeConfig(t, "version: 1\nmonitoring:\n  admit_session_matches: true\n")
	_, _, err = Load(path)
	if err == nil || !strings.Contains(err.Error(), "monitoring.admit_session_matches") || !strings.Contains(err.Error(), "providers.session_file_channel") {
		t.Fatalf("Load() error = %v, want admission prerequisite naming both keys", err)
	}
}

func TestLoadRejectsAdmissionForTmux(t *testing.T) {
	path := writeConfig(t, "version: 1\nruntime:\n  type: tmux\nproviders:\n  session_file_channel: true\nmonitoring:\n  admit_session_matches: true\n")
	_, _, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "admit_session_matches") || !strings.Contains(err.Error(), "tmux") {
		t.Fatalf("Load() error = %v, want tmux admission rejection", err)
	}
}

func TestLoadRejectsSessionFileChannelForTmux(t *testing.T) {
	path := writeConfig(t, "version: 1\nruntime:\n  type: tmux\nproviders:\n  session_file_channel: true\n")
	_, _, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "session_file_channel") || !strings.Contains(err.Error(), "tmux") {
		t.Fatalf("Load() error = %v, want tmux session-file rejection", err)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	_, _, err := Load(writeConfig(t, "version: 1\nunknown: true\n"))
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("Load() error = %v, want unknown key", err)
	}
}

func TestLoadRequiresVersionOne(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want string
	}{
		{name: "missing", data: "runtime: {}\n", want: "version"},
		{name: "wrong", data: "version: 2\n", want: "version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Load(writeConfig(t, tc.data))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadRejectsBadDurationTransportAndProvider(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want string
	}{
		{name: "duration", data: "version: 1\nmonitoring:\n  interval: nope\n", want: "interval"},
		{name: "transport", data: "version: 1\nruntime:\n  transport: bogus\n", want: "transport"},
		{name: "provider", data: "version: 1\nproviders:\n  enabled: [gemini]\n", want: "provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Load(writeConfig(t, tc.data))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("Load() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadAbsentReturnsZeroAndNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")
	got, found, err := Load(path)
	if err != nil || found || !reflect.DeepEqual(got, Config{}) {
		t.Fatalf("Load() = %#v, found=%v, err=%v; want zero and not found", got, found, err)
	}
}

func TestDefaultPathHonorsXDGConfigHome(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	want := filepath.Join(configHome, "herdr-auto-resume", "config.yaml")
	if got := DefaultPath(); got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}
