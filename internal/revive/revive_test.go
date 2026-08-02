package revive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/sessionfile"
)

const reviveSessionID = "11111111-1111-4111-8111-111111111111"

type fakeRuntime struct {
	panes []runtimeapi.Pane
	reads int
}

type sequencedRuntime struct {
	snapshots [][]runtimeapi.Pane
	index     int
}

func (r *sequencedRuntime) ListPanes() ([]runtimeapi.Pane, error) {
	if len(r.snapshots) == 0 {
		return nil, nil
	}
	index := r.index
	if index >= len(r.snapshots) {
		index = len(r.snapshots) - 1
	}
	r.index++
	return append([]runtimeapi.Pane(nil), r.snapshots[index]...), nil
}

func (r *fakeRuntime) ListPanes() ([]runtimeapi.Pane, error) {
	r.reads++
	return append([]runtimeapi.Pane(nil), r.panes...), nil
}

type fakeSpawner struct {
	workspace Workspace
	creates   int
	runs      [][]string
	onRun     func()
}

func (s *fakeSpawner) CreateWorkspace(label, cwd string) (Workspace, error) {
	s.creates++
	if label != "herdr-auto-resume-revive" || cwd != "/tmp/revive" {
		return Workspace{}, fmt.Errorf("unexpected workspace request %q %q", label, cwd)
	}
	return s.workspace, nil
}

func (s *fakeSpawner) RunPane(paneID string, args ...string) error {
	s.runs = append(s.runs, append([]string{paneID}, args...))
	if s.onRun != nil {
		s.onRun()
	}
	return nil
}

func TestOperatorHappyPathPersistsAttachedAfterSpawn(t *testing.T) {
	scanner := testReviveScanner(t)
	writeReviveSessionForOperator(t, scanner)
	rt := &fakeRuntime{}
	spawner := &fakeSpawner{workspace: Workspace{WorkspaceID: "ws-1", PaneID: "pane-1"}}
	spawner.onRun = func() {
		rt.panes = []runtimeapi.Pane{{ID: "pane-1", WorkspaceID: "ws-1", AgentSessionID: reviveSessionID}}
	}
	op := testOperator(scanner, rt, spawner)
	var output strings.Builder
	if err := op.Run("11111111-1111-4111-8111-111", &output); err != nil {
		t.Fatal(err)
	}
	if spawner.creates != 1 || len(spawner.runs) != 1 || strings.Join(spawner.runs[0], " ") != "pane-1 claude --resume "+reviveSessionID {
		t.Fatalf("unexpected spawn calls: %#v", spawner)
	}
	if !strings.Contains(output.String(), "no continue was sent") {
		t.Fatalf("missing explicit no-continue output: %q", output.String())
	}
	intents, err := scanner.ReviveIntents()
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].State != sessionfile.ReviveAttached || intents[0].PaneID != "pane-1" {
		t.Fatalf("unexpected final intents: %#v", intents)
	}
}

func TestOperatorVetoesLivePaneBeforePersistingOrSpawning(t *testing.T) {
	scanner := testReviveScanner(t)
	writeReviveSessionForOperator(t, scanner)
	rt := &fakeRuntime{panes: []runtimeapi.Pane{{ID: "live-pane", AgentSessionID: reviveSessionID}}}
	spawner := &fakeSpawner{workspace: Workspace{WorkspaceID: "ws-1", PaneID: "pane-1"}}
	err := testOperator(scanner, rt, spawner).Run(reviveSessionID[:8], io.Discard)
	if err == nil || !strings.Contains(err.Error(), "attached to pane live-pane") {
		t.Fatalf("expected double-attach veto, got %v", err)
	}
	if spawner.creates != 0 {
		t.Fatalf("spawned despite veto: %d", spawner.creates)
	}
}

func TestOperatorRechecksPaneAfterIntentBeforeSpawning(t *testing.T) {
	scanner := testReviveScanner(t)
	writeReviveSessionForOperator(t, scanner)
	rt := &sequencedRuntime{snapshots: [][]runtimeapi.Pane{
		{},
		{{ID: "appeared-pane", WorkspaceID: "ws-existing", AgentSessionID: reviveSessionID}},
	}}
	spawner := &fakeSpawner{workspace: Workspace{WorkspaceID: "ws-1", PaneID: "pane-1"}}
	err := testOperator(scanner, rt, spawner).Run(reviveSessionID[:8], io.Discard)
	if err == nil || !strings.Contains(err.Error(), "before revive spawn") {
		t.Fatalf("expected pre-spawn race veto, got %v", err)
	}
	if spawner.creates != 0 {
		t.Fatalf("created workspace after pane appeared: %d", spawner.creates)
	}
	intents, readErr := scanner.ReviveIntents()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(intents) != 1 || intents[0].State != sessionfile.ReviveAttached || intents[0].PaneID != "appeared-pane" {
		t.Fatalf("race veto did not reconcile intent: %#v", intents)
	}
}

func TestOperatorLeaseConflictFailsFastWithHolder(t *testing.T) {
	scanner := testReviveScanner(t)
	writeReviveSessionForOperator(t, scanner)
	lease, err := AcquireSessionLease(scannerState(scanner), reviveSessionID, 4242)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	err = testOperator(scanner, &fakeRuntime{}, &fakeSpawner{}).Run(reviveSessionID[:8], io.Discard)
	if err == nil || !strings.Contains(err.Error(), "holder pid 4242") {
		t.Fatalf("expected lease conflict, got %v", err)
	}
}

func TestOperatorReportsAmbiguousAndUnknownPrefixesWithCandidates(t *testing.T) {
	scanner := testReviveScanner(t)
	writeReviveSessionForOperator(t, scanner)
	writeSessionForOperator(t, scanner, "11111111-1111-4111-8111-222222222222", "/tmp/other")
	op := testOperator(scanner, &fakeRuntime{}, &fakeSpawner{})
	for _, prefix := range []string{"11111111-1111-4111-8111-", "99999999"} {
		err := op.Run(prefix, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "candidates") || !strings.Contains(err.Error(), reviveSessionID) {
			t.Fatalf("prefix %q error did not list candidates: %v", prefix, err)
		}
	}
}

func TestReconcileStaleIntentBeforeSpawnAndAfterSpawn(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	for name, panes := range map[string][]runtimeapi.Pane{
		"before spawn clears":  nil,
		"after spawn attaches": {{ID: "existing-pane", WorkspaceID: "ws-existing", AgentSessionID: reviveSessionID}},
	} {
		t.Run(name, func(t *testing.T) {
			scanner := testReviveScannerAt(t, now)
			if err := scanner.BeginRevive(sessionfile.ReviveIntent{SessionID: reviveSessionID, Timestamp: now.Add(-time.Hour), LeasePID: 1, State: sessionfile.ReviveAttaching}); err != nil {
				t.Fatal(err)
			}
			rt := &fakeRuntime{panes: panes}
			op := testOperatorAt(scanner, rt, &fakeSpawner{}, now)
			if err := op.Reconcile(); err != nil {
				t.Fatal(err)
			}
			intents, err := scanner.ReviveIntents()
			if err != nil {
				t.Fatal(err)
			}
			if panes == nil && len(intents) != 0 {
				t.Fatalf("stale unattached intent survived: %#v", intents)
			}
			if panes != nil && (len(intents) != 1 || intents[0].State != sessionfile.ReviveAttached || intents[0].PaneID != "existing-pane") {
				t.Fatalf("stale attached intent not reconciled: %#v", intents)
			}
		})
	}
}

func testReviveScanner(t *testing.T) *sessionfile.Scanner {
	return testReviveScannerAt(t, time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC))
}

func testReviveScannerAt(t *testing.T, now time.Time) *sessionfile.Scanner {
	t.Helper()
	scanner, err := sessionfile.New(sessionfile.Config{RootDir: t.TempDir(), StatePath: filepath.Join(t.TempDir(), "watcher.state"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return scanner
}

func scannerState(scanner *sessionfile.Scanner) string {
	return scanner.StatePath()
}

func testOperator(scanner *sessionfile.Scanner, rt PaneRuntime, spawner Spawner) *Operator {
	return testOperatorAt(scanner, rt, spawner, time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC))
}

func testOperatorAt(scanner *sessionfile.Scanner, rt PaneRuntime, spawner Spawner, now time.Time) *Operator {
	return New(Config{Scanner: scanner, Runtime: rt, Spawner: spawner, Now: func() time.Time { return now }, Grace: time.Minute, AttachTimeout: time.Second, PollInterval: time.Millisecond, Sleep: func(time.Duration) {}})
}

func writeReviveSessionForOperator(t *testing.T, scanner *sessionfile.Scanner) {
	writeSessionForOperator(t, scanner, reviveSessionID, "/tmp/revive")
}

func writeSessionForOperator(t *testing.T, scanner *sessionfile.Scanner, id, cwd string) {
	t.Helper()
	root := scanner.RootDir()
	project := filepath.Join(root, "projects", "-tmp-revive")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"sessionId":"` + id + `","cwd":"` + cwd + `","timestamp":"2026-08-02T12:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(project, id+".jsonl"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOperatorRetainsAttachingOnSpawnFailure(t *testing.T) {
	scanner := testReviveScanner(t)
	writeReviveSessionForOperator(t, scanner)
	spawner := &errorSpawner{err: errors.New("spawn failed")}
	err := testOperator(scanner, &fakeRuntime{}, spawner).Run(reviveSessionID[:8], io.Discard)
	if err == nil {
		t.Fatal("expected spawn failure")
	}
	intents, readErr := scanner.ReviveIntents()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(intents) != 1 || intents[0].State != sessionfile.ReviveAttaching {
		t.Fatalf("spawn failure did not leave attaching intent: %#v", intents)
	}
}

type errorSpawner struct{ err error }

func (s *errorSpawner) CreateWorkspace(string, string) (Workspace, error) { return Workspace{}, s.err }
func (s *errorSpawner) RunPane(string, ...string) error                   { return s.err }
