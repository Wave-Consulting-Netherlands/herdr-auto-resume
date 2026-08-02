package herdr

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	runtimeapi "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

type recordedRunner struct {
	args []([]string)
	out  []byte
	err  error
}

func (r *recordedRunner) run(args ...string) ([]byte, error) {
	r.args = append(r.args, append([]string(nil), args...))
	return r.out, r.err
}

func testAdapter(o Options, r *recordedRunner) *Adapter {
	a := New(o)
	a.run = r.run
	return a
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestNewAppliesDefaults(t *testing.T) {
	a := New(Options{})
	if a.options.Bin != "herdr" || a.options.ReadSource != "detection" {
		t.Fatalf("options = %#v, want herdr/recent defaults", a.options)
	}
}

func TestListPanesDecodesEnvelopeAndBuildsArguments(t *testing.T) {
	r := &recordedRunner{out: fixture(t, "pane_list.json")}
	a := testAdapter(Options{Session: "session-1", Workspace: "workspace-1"}, r)

	panes, err := a.ListPanes()
	if err != nil {
		t.Fatalf("ListPanes: %v", err)
	}
	if len(panes) != 6 || panes[0] != (runtimeapi.Pane{ID: "w1:p1", TerminalID: "term_000000000000000", WorkspaceID: "w1", Title: "project shell", CWD: "/home/user/project", Agent: "claude", AgentSessionID: "ce7bb791-f92c-4edb-b795-08a4fff2b778"}) {
		t.Fatalf("panes = %#v", panes)
	}
	if panes[1].AgentSessionID != "" {
		t.Fatalf("pane without agent_session = %#v", panes[1])
	}
	if want := []string{"pane", "list", "--workspace", "workspace-1", "--session", "session-1"}; !reflect.DeepEqual(r.args[0], want) {
		t.Fatalf("argv = %#v, want %#v", r.args[0], want)
	}
}

func TestProcessInfoUsesFirstForegroundProcess(t *testing.T) {
	r := &recordedRunner{out: fixture(t, "process_info.json")}
	a := testAdapter(Options{}, r)

	info, err := a.ProcessInfo("w1:p1")
	if err != nil {
		t.Fatalf("ProcessInfo: %v", err)
	}
	if want := (runtimeapi.ProcessInfo{Command: "node", CWD: "/home/user/project"}); info != want {
		t.Fatalf("process info = %#v, want %#v", info, want)
	}
	if want := []string{"pane", "process-info", "--pane", "w1:p1"}; !reflect.DeepEqual(r.args[0], want) {
		t.Fatalf("argv = %#v, want %#v", r.args[0], want)
	}
}

func TestProcessInfoFallsBackToCmdline(t *testing.T) {
	r := &recordedRunner{out: []byte(`{"result":{"process_info":{"foreground_processes":[{"cmdline":"shell -i","cwd":"/home/user/project"}]}}}`)}
	a := testAdapter(Options{}, r)

	info, err := a.ProcessInfo("p1")
	if err != nil {
		t.Fatalf("ProcessInfo: %v", err)
	}
	if info.Command != "shell -i" || info.CWD != "/home/user/project" {
		t.Fatalf("process info = %#v", info)
	}
}

func TestReadPaneReturnsPlainTextUnchanged(t *testing.T) {
	want := fixture(t, "pane_read.json")
	r := &recordedRunner{out: want}
	a := testAdapter(Options{ReadSource: "recent"}, r)

	got, err := a.ReadPane("w1:p1", 42)
	if err != nil {
		t.Fatalf("ReadPane: %v", err)
	}
	if got != string(want) {
		t.Fatalf("read output changed: got %q, want %q", got, want)
	}
	if wantArgs := []string{"pane", "read", "w1:p1", "--source", "recent", "--lines", "42"}; !reflect.DeepEqual(r.args[0], wantArgs) {
		t.Fatalf("argv = %#v, want %#v", r.args[0], wantArgs)
	}
}

func TestReadPaneOmitsNonPositiveLines(t *testing.T) {
	r := &recordedRunner{}
	a := testAdapter(Options{}, r)
	if _, err := a.ReadPane("p1", 0); err != nil {
		t.Fatalf("ReadPane: %v", err)
	}
	if want := []string{"pane", "read", "p1", "--source", "detection"}; !reflect.DeepEqual(r.args[0], want) {
		t.Fatalf("argv = %#v, want %#v", r.args[0], want)
	}
}

func TestSendKeysTranslatesCanonicalNames(t *testing.T) {
	r := &recordedRunner{}
	a := testAdapter(Options{}, r)
	if err := a.SendKeys("p1", runtimeapi.KeyEscape, runtimeapi.KeyEnter, "ctrl-c"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	want := []string{"pane", "send-keys", "p1", "esc", "enter", "ctrl-c"}
	if !reflect.DeepEqual(r.args[0], want) {
		t.Fatalf("argv = %#v, want %#v", r.args[0], want)
	}
}

func TestSendTextAndNotifyUseExactArgumentBoundaries(t *testing.T) {
	r := &recordedRunner{}
	a := testAdapter(Options{}, r)
	text := "foo; rm -rf /"
	if err := a.SendText("p1", text); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if err := a.Notify("title", "body"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if err := a.Notify("title", ""); err != nil {
		t.Fatalf("Notify without body: %v", err)
	}
	want := [][]string{
		{"pane", "send-text", "p1", text},
		{"notification", "show", "title", "--body", "body"},
		{"notification", "show", "title"},
	}
	if !reflect.DeepEqual(r.args, want) {
		t.Fatalf("argv = %#v, want %#v", r.args, want)
	}
}

func TestErrorEnvelopeMapsEvenWhenCommandExits(t *testing.T) {
	r := &recordedRunner{out: fixture(t, "error_response.json"), err: errors.New("exit status 1")}
	a := testAdapter(Options{}, r)

	_, err := a.ReadPane("nonexistent:pane", 0)
	var herdrErr HerdrError
	if !errors.As(err, &herdrErr) {
		t.Fatalf("error = %v, want HerdrError", err)
	}
	if herdrErr.Code != "pane_not_found" || herdrErr.Message != "pane nonexistent:pane not found" {
		t.Fatalf("HerdrError = %#v", herdrErr)
	}
}

func TestMalformedFailureWrapsExitError(t *testing.T) {
	exitErr := errors.New("exit status 1")
	r := &recordedRunner{out: []byte("not json"), err: exitErr}
	a := testAdapter(Options{}, r)
	_, err := a.ReadPane("p1", 0)
	if !errors.Is(err, exitErr) || !strings.Contains(err.Error(), "herdr command failed") {
		t.Fatalf("error = %v, want wrapped exit error", err)
	}
}

func TestSelfPaneIDReadsOwnEnvironment(t *testing.T) {
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	if got, err := New(Options{}).SelfPaneID(); err != nil || got != "w1:p1" {
		t.Fatalf("SelfPaneID = %q, %v", got, err)
	}
	t.Setenv("HERDR_PANE_ID", "")
	if got, err := New(Options{}).SelfPaneID(); err != nil || got != "" {
		t.Fatalf("unset SelfPaneID = %q, %v", got, err)
	}
}

func TestReviveSpawnerUsesWorkspaceAndPaneRunCommands(t *testing.T) {
	var calls [][]string
	adapter := NewWithExec(Options{}, func(args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(calls) == 1 {
			return []byte(`{"result":{"workspace":{"workspace_id":"ws-1","panes":[{"pane_id":"pane-1"}]}}}`), nil
		}
		return []byte(`{"result":{}}`), nil
	})
	workspace, err := adapter.CreateWorkspace("herdr-auto-resume-revive", "/tmp/revive")
	if err != nil {
		t.Fatal(err)
	}
	if workspace.WorkspaceID != "ws-1" || workspace.PaneID != "pane-1" {
		t.Fatalf("unexpected workspace: %#v", workspace)
	}
	if err := adapter.RunPane(workspace.PaneID, "claude", "--resume", "11111111-1111-4111-8111-111111111111"); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(calls[0], " "), "workspace create --label herdr-auto-resume-revive --cwd /tmp/revive"; got != want {
		t.Fatalf("create args = %q, want %q", got, want)
	}
	if got, want := strings.Join(calls[1], " "), "pane run pane-1 claude --resume 11111111-1111-4111-8111-111111111111"; got != want {
		t.Fatalf("run args = %q, want %q", got, want)
	}
}

func TestChildEnvironmentScrubsHerdrVariables(t *testing.T) {
	input := []string{"PATH=/bin", "HERDR_SOCKET_PATH=/unsafe.sock", "HERDR_PANE_ID=w1:p1", "HERDR_ENV=ci", "OTHER=value"}
	got := scrubEnvironment(input, "/safe.sock")
	if !reflect.DeepEqual(got, []string{"PATH=/bin", "OTHER=value", "HERDR_SOCKET_PATH=/safe.sock"}) {
		t.Fatalf("scrubbed env = %#v", got)
	}
	if got := scrubEnvironment(input, ""); !reflect.DeepEqual(got, []string{"PATH=/bin", "OTHER=value"}) {
		t.Fatalf("scrubbed env without socket = %#v", got)
	}
}

func TestReviveSpawnRunnerScrubsInheritedHerdrEnvironment(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "herdr-fake")
	capture := filepath.Join(dir, "capture")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$REVIVE_CAPTURE\"\nenv >> \"$REVIVE_CAPTURE\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REVIVE_CAPTURE", capture)
	t.Setenv("HERDR_SOCKET_PATH", "/unsafe.sock")
	if _, err := productionRunner(Options{Bin: bin, SocketPath: "/safe.sock"})("pane", "run", "p1", "claude", "--resume", "session"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "HERDR_SOCKET_PATH=/unsafe.sock") || !strings.Contains(text, "HERDR_SOCKET_PATH=/safe.sock") {
		t.Fatalf("spawn environment was not scrubbed: %q", text)
	}
}

func TestHerdrName(t *testing.T) {
	if got := New(Options{}).Name(); got != "herdr" {
		t.Fatalf("Name = %q, want herdr", got)
	}
}

var _ runtimeapi.Runtime = (*Adapter)(nil)
