package herdr

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	runtimeapi "github.com/walt-verweij/herdr-auto-resume/internal/runtime"
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
	if a.options.Bin != "herdr" || a.options.ReadSource != "recent" {
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
	if len(panes) != 6 || panes[0] != (runtimeapi.Pane{ID: "w1:p1", Title: "project shell", Agent: "claude"}) {
		t.Fatalf("panes = %#v", panes)
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
	if want := []string{"pane", "read", "p1", "--source", "recent"}; !reflect.DeepEqual(r.args[0], want) {
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

func TestHerdrName(t *testing.T) {
	if got := New(Options{}).Name(); got != "herdr" {
		t.Fatalf("Name = %q, want herdr", got)
	}
}

var _ runtimeapi.Runtime = (*Adapter)(nil)
