package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	herdradapter "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/herdr"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func startDoctorSocket(t *testing.T, protocol int, acknowledge bool) string {
	t.Helper()
	// t.TempDir() embeds the full test/subtest name; long names push the
	// socket path past the 108-byte sun_path limit (bind: invalid argument
	// on CI). Use a short MkdirTemp dir instead.
	dir, err := os.MkdirTemp("", "hard*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			go func(conn *net.UnixConn) {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var request struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				if json.Unmarshal(line, &request) != nil {
					return
				}
				var result any
				switch request.Method {
				case "ping":
					result = map[string]any{"type": "pong", "protocol": protocol, "version": "0.7.5"}
				case "session.snapshot":
					result = map[string]any{"type": "snapshot", "protocol": protocol, "snapshot": map[string]any{"panes": []any{}}}
				case "events.subscribe":
					if acknowledge {
						result = map[string]any{"type": "subscription_started"}
					} else {
						result = map[string]any{"type": "not_started"}
					}
				}
				payload, _ := json.Marshal(map[string]any{"id": json.RawMessage(request.ID), "result": result})
				_, _ = conn.Write(append(payload, '\n'))
			}(conn)
		}
	}()
	return path
}

func passingDoctorDeps() doctorDeps {
	return doctorDeps{
		resolve: func(string) (string, error) { return "/usr/bin/herdr", nil },
		run: func(_ string, args ...string) ([]byte, error) {
			switch strings.Join(args, " ") {
			case "--version":
				return []byte("herdr 0.7.5\n"), nil
			case "status":
				return []byte(`{"protocol":17}`), nil
			case "api schema --json":
				return []byte(`{"schema":"ok"}`), nil
			default:
				return nil, nil
			}
		},
		socket: func(string) error { return nil },
		home:   func() (string, error) { return "/home/user", nil },
		newAdapter: func(herdradapter.Options, herdradapter.ExecFunc) runtime.Runtime {
			return &runtime.Fake{PanesList: []runtime.Pane{{ID: "w1:p1"}}}
		},
	}
}

func isolateDoctorState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestDoctorReportPassesAllChecks(t *testing.T) {
	isolateDoctorState(t)
	var out bytes.Buffer
	if got := runDoctorCommand([]string{"--transport", "cli"}, &out, passingDoctorDeps()); got != 0 {
		t.Fatalf("doctor exit = %d, want 0\n%s", got, out.String())
	}
	if first := strings.SplitN(out.String(), "\n", 2)[0]; !strings.HasPrefix(first, "INFO version: herdr-auto-resume ") {
		t.Fatalf("first doctor line = %q, want version info", first)
	}
	if strings.Contains(out.String(), "FAIL") || !strings.Contains(out.String(), "PASS binary") || !strings.Contains(out.String(), "PASS adapter") {
		t.Fatalf("report = %q", out.String())
	}
}

func TestDoctorVersionLinePreservesCLIReportBody(t *testing.T) {
	isolateDoctorState(t)
	t.Setenv("HERDR_PANE_ID", "")
	oldVersion, oldCommit, oldDate := version, commit, date
	t.Cleanup(func() { version, commit, date = oldVersion, oldCommit, oldDate })
	version, commit, date = "v0.2.0", "abc1234", "2026-07-31T12:00:00Z"

	var out bytes.Buffer
	if got := runDoctorCommand([]string{"--transport", "cli"}, &out, passingDoctorDeps()); got != 0 {
		t.Fatalf("doctor exit = %d\n%s", got, out.String())
	}
	lines := strings.SplitN(out.String(), "\n", 2)
	if lines[0] != "INFO version: herdr-auto-resume v0.2.0 (abc1234)" {
		t.Fatalf("version line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "INFO config: none\nINFO watcher: none on ") {
		t.Fatalf("config/watcher lines = %q", lines[1])
	}
	body := strings.SplitN(lines[1], "\n", 3)
	wantBody := "PASS binary: /usr/bin/herdr (herdr 0.7.5)\n" +
		"PASS socket: /home/user/.config/herdr/herdr.sock\n" +
		"PASS status: protocol 17\n" +
		"PASS adapter: decoded 1 panes\n" +
		"PASS schema: valid JSON\n" +
		"WARN self: HERDR_PANE_ID is unset; not running inside a herdr pane\n"
	if len(lines) != 2 || len(body) != 3 || body[2] != wantBody {
		t.Fatalf("doctor body changed: %q", strings.Join(body[2:], "\n"))
	}
}

func TestDoctorVersionLineIsFirstInSocketReport(t *testing.T) {
	isolateDoctorState(t)
	oldVersion, oldCommit, oldDate := version, commit, date
	t.Cleanup(func() { version, commit, date = oldVersion, oldCommit, oldDate })
	version, commit, date = "v0.2.0", "abc1234", "2026-07-31T12:00:00Z"

	path := startDoctorSocket(t, 17, true)
	var out bytes.Buffer
	if got := runDoctorCommand([]string{"--transport", "socket", "--socket", path}, &out, passingDoctorDeps()); got != 0 {
		t.Fatalf("doctor socket exit = %d\n%s", got, out.String())
	}
	first := strings.SplitN(out.String(), "\n", 2)[0]
	if first != "INFO version: herdr-auto-resume v0.2.0 (abc1234)" {
		t.Fatalf("first socket doctor line = %q", first)
	}
}

func TestDoctorReportsWatcherLockState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Run("none", func(t *testing.T) {
		var out bytes.Buffer
		if got := runDoctorCommand([]string{"--transport", "cli", "--state-file", statePath}, &out, passingDoctorDeps()); got != 0 {
			t.Fatalf("doctor exit = %d\n%s", got, out.String())
		}
		if !strings.Contains(out.String(), "INFO watcher: none on "+statePath) {
			t.Fatalf("report = %q", out.String())
		}
	})
	t.Run("active", func(t *testing.T) {
		lock, err := store.AcquireRunLock(statePath)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Release()
		var out bytes.Buffer
		if got := runDoctorCommand([]string{"--transport", "cli", "--state-file", statePath}, &out, passingDoctorDeps()); got != 0 {
			t.Fatalf("doctor exit = %d\n%s", got, out.String())
		}
		if !strings.Contains(out.String(), "INFO watcher: active (pid "+strconv.Itoa(os.Getpid())+") on "+statePath) {
			t.Fatalf("report = %q", out.String())
		}
	})
}

func TestDoctorReportsConfigState(t *testing.T) {
	isolateDoctorState(t)
	t.Run("absent", func(t *testing.T) {
		var out bytes.Buffer
		if got := runDoctorCommand([]string{"--transport", "cli"}, &out, passingDoctorDeps()); got != 0 {
			t.Fatalf("doctor exit = %d\n%s", got, out.String())
		}
		if !strings.Contains(out.String(), "INFO config: none") {
			t.Fatalf("report = %q", out.String())
		}
	})
	t.Run("valid", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("version: 1\nstate:\n  file: /tmp/doctor-state.json\n"), 0600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if got := runDoctorCommand([]string{"--transport", "cli", "--config", path}, &out, passingDoctorDeps()); got != 0 {
			t.Fatalf("doctor exit = %d\n%s", got, out.String())
		}
		if !strings.Contains(out.String(), "PASS config: "+path) || !strings.Contains(out.String(), "INFO watcher: none on /tmp/doctor-state.json") {
			t.Fatalf("report = %q", out.String())
		}
	})
	t.Run("invalid", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte("version: 2\n"), 0600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if got := runDoctorCommand([]string{"--transport", "cli", "--config", path}, &out, passingDoctorDeps()); got == 0 {
			t.Fatalf("doctor exit = %d\n%s", got, out.String())
		}
		if !strings.Contains(out.String(), "FAIL config:") || !strings.Contains(out.String(), "version") {
			t.Fatalf("report = %q", out.String())
		}
	})
}

func TestDoctorReportFailsWhenRequiredCheckFails(t *testing.T) {
	isolateDoctorState(t)
	deps := passingDoctorDeps()
	deps.resolve = func(string) (string, error) { return "", errors.New("not found") }
	deps.socket = func(string) error { return errors.New("missing") }
	deps.run = func(_ string, _ ...string) ([]byte, error) { return nil, errors.New("unavailable") }
	deps.newAdapter = func(herdradapter.Options, herdradapter.ExecFunc) runtime.Runtime {
		return &runtime.Fake{Errs: map[string]error{"ListPanes": errors.New("list failed")}}
	}
	var out bytes.Buffer
	if got := runDoctorCommand([]string{"--transport", "cli"}, &out, deps); got == 0 {
		t.Fatalf("doctor exit = %d, want nonzero\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "FAIL binary") || !strings.Contains(out.String(), "FAIL socket") || !strings.Contains(out.String(), "FAIL status") || !strings.Contains(out.String(), "FAIL adapter") {
		t.Fatalf("report = %q", out.String())
	}
}

func TestDoctorSchemaParseFailureIsWarning(t *testing.T) {
	isolateDoctorState(t)
	deps := passingDoctorDeps()
	deps.run = func(_ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "api schema --json" {
			return []byte("{\x01}"), nil
		}
		return passingDoctorDeps().run("herdr", args...)
	}
	var out bytes.Buffer
	if got := runDoctorCommand([]string{"--transport", "cli"}, &out, deps); got != 0 {
		t.Fatalf("doctor exit = %d, want 0\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "WARN schema") {
		t.Fatalf("report = %q, want schema warning", out.String())
	}
}

func TestDoctorUnknownProtocolIsWarningNotPass(t *testing.T) {
	isolateDoctorState(t)
	deps := passingDoctorDeps()
	deps.run = func(_ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "status" {
			return []byte(`{"status":"ok"}`), nil
		}
		return passingDoctorDeps().run("herdr", args...)
	}
	var out bytes.Buffer
	if got := runDoctorCommand([]string{"--transport", "cli"}, &out, deps); got != 0 {
		t.Fatalf("doctor exit = %d, want warning-only success\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "WARN status: protocol unknown") || strings.Contains(out.String(), "PASS status: protocol 17") {
		t.Fatalf("report = %q, want unknown protocol warning", out.String())
	}
}

func TestDoctorSocketModePassesFakeRoundTrips(t *testing.T) {
	isolateDoctorState(t)
	path := startDoctorSocket(t, 17, true)
	var out bytes.Buffer
	if got := runDoctorCommand([]string{"--transport", "socket", "--socket", path}, &out, passingDoctorDeps()); got != 0 {
		t.Fatalf("doctor socket exit = %d\n%s", got, out.String())
	}
	if strings.Contains(out.String(), "FAIL") || !strings.Contains(out.String(), "PASS ping: protocol 17") || !strings.Contains(out.String(), "PASS events") {
		t.Fatalf("socket report = %q", out.String())
	}
}

func TestDoctorSocketModeRefusedProtocolWarningAndMissingAck(t *testing.T) {
	isolateDoctorState(t)
	t.Run("refused", func(t *testing.T) {
		var out bytes.Buffer
		if got := runDoctorCommand([]string{"--transport", "socket", "--socket", filepath.Join(t.TempDir(), "missing.sock")}, &out, passingDoctorDeps()); got == 0 || !strings.Contains(out.String(), "FAIL ping") {
			t.Fatalf("refused exit=%d report=%q", got, out.String())
		}
	})
	t.Run("protocol warning", func(t *testing.T) {
		path := startDoctorSocket(t, 16, true)
		var out bytes.Buffer
		if got := runDoctorCommand([]string{"--transport", "socket", "--socket", path}, &out, passingDoctorDeps()); got != 0 || !strings.Contains(out.String(), "WARN ping: protocol 16") {
			t.Fatalf("protocol exit=%d report=%q", got, out.String())
		}
	})
	t.Run("missing acknowledgement", func(t *testing.T) {
		path := startDoctorSocket(t, 17, false)
		var out bytes.Buffer
		if got := runDoctorCommand([]string{"--transport", "socket", "--socket", path}, &out, passingDoctorDeps()); got == 0 || !strings.Contains(out.String(), "FAIL events") {
			t.Fatalf("ack exit=%d report=%q", got, out.String())
		}
	})
}

func TestDoctorTransportResolutionMatrix(t *testing.T) {
	isolateDoctorState(t)
	cases := []struct {
		name        string
		args        []string
		config      string
		wantSocket  bool
		wantWarning string
		wantErr     string
	}{
		{name: "default herdr", wantSocket: true},
		{name: "default herdr session", args: []string{"--session", "s1"}, wantWarning: "--session"},
		{name: "default tmux", args: []string{"--runtime", "tmux"}, wantWarning: "--runtime"},
		{name: "default tmux session", args: []string{"--runtime", "tmux", "--session", "s1"}, wantWarning: "--runtime"},
		{name: "yaml herdr cli", config: "runtime:\n  type: herdr\n  transport: cli\n"},
		{name: "yaml herdr socket", config: "runtime:\n  type: herdr\n  transport: socket\n", wantSocket: true},
		{name: "yaml herdr socket session", config: "runtime:\n  type: herdr\n  transport: socket\n", args: []string{"--session", "s1"}, wantErr: "--session is unsupported"},
		{name: "yaml tmux cli", config: "runtime:\n  type: tmux\n  transport: cli\n"},
		{name: "yaml tmux socket", config: "runtime:\n  type: tmux\n  transport: socket\n", wantErr: "runtime.transport socket requires runtime.type herdr"},
		{name: "flag herdr cli", args: []string{"--transport", "cli"}},
		{name: "flag herdr socket", args: []string{"--transport", "socket"}, wantSocket: true},
		{name: "flag socket session", args: []string{"--transport", "socket", "--session", "s1"}, wantErr: "--session is unsupported"},
		{name: "flag socket tmux", args: []string{"--transport", "socket", "--runtime", "tmux"}, wantErr: "requires --runtime herdr"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string(nil), tc.args...)
			if tc.config != "" {
				path := filepath.Join(t.TempDir(), "config.yaml")
				if err := os.WriteFile(path, []byte("version: 1\n"+tc.config), 0600); err != nil {
					t.Fatal(err)
				}
				args = append([]string{"--config", path}, args...)
			}
			var socketPath string
			if tc.wantSocket {
				socketPath = startDoctorSocket(t, 17, true)
				args = append(args, "--socket", socketPath)
			}
			var out bytes.Buffer
			got := runDoctorCommand(args, &out, passingDoctorDeps())
			if tc.wantErr != "" {
				if got == 0 || !strings.Contains(out.String(), tc.wantErr) {
					t.Fatalf("exit=%d report=%q; want error %q", got, out.String(), tc.wantErr)
				}
				return
			}
			if got != 0 {
				t.Fatalf("doctor exit=%d report=%q", got, out.String())
			}
			if tc.wantSocket && !strings.Contains(out.String(), "PASS ping") {
				t.Fatalf("report=%q, want socket checks", out.String())
			}
			if !tc.wantSocket && !strings.Contains(out.String(), "PASS binary") {
				t.Fatalf("report=%q, want CLI checks", out.String())
			}
			if tc.wantWarning != "" && !strings.Contains(out.String(), tc.wantWarning) {
				t.Fatalf("report=%q, want warning naming %q", out.String(), tc.wantWarning)
			}
		})
	}
}

func TestDoctorExplicitCLITransportMatchesTheCLIPath(t *testing.T) {
	isolateDoctorState(t)
	var explicit bytes.Buffer
	if got := runDoctorCommand([]string{"--transport", "cli"}, &explicit, passingDoctorDeps()); got != 0 {
		t.Fatalf("explicit doctor exit = %d", got)
	}
	if !strings.Contains(explicit.String(), "PASS binary") || strings.Contains(explicit.String(), "PASS ping") {
		t.Fatalf("explicit CLI output = %q", explicit.String())
	}
}
