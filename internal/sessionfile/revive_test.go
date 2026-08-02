package sessionfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverSessionsReturnsUniquePrefixCandidatesAndCWD(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "watcher.state")
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	for id, cwd := range map[string]string{
		"11111111-1111-4111-8111-111111111111": "/tmp/one",
		"11111111-1111-4111-8111-222222222222": "/tmp/two",
	} {
		writeReviveSession(t, root, id, cwd)
	}
	scanner, err := New(Config{RootDir: root, StatePath: state, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := scanner.DiscoverSessions("11111111-1111-4111-8111-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].SessionID != "11111111-1111-4111-8111-111111111111" || candidates[0].CWD != "/tmp/one" {
		t.Fatalf("unexpected candidates: %#v", candidates)
	}
	all, err := scanner.DiscoverSessions("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected all session candidates, got %#v", all)
	}
}

func TestReviveIntentRoundTripUsesScannerSidecar(t *testing.T) {
	state := filepath.Join(t.TempDir(), "watcher.state")
	scanner, err := New(Config{RootDir: t.TempDir(), StatePath: state})
	if err != nil {
		t.Fatal(err)
	}
	intent := ReviveIntent{
		SessionID: "11111111-1111-4111-8111-111111111111",
		Timestamp: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
		LeasePID:  os.Getpid(),
		State:     ReviveAttaching,
	}
	if err := scanner.BeginRevive(intent); err != nil {
		t.Fatal(err)
	}
	intents, err := scanner.ReviveIntents()
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0] != intent {
		t.Fatalf("unexpected intent: %#v", intents)
	}
	if err := scanner.CompleteRevive(intent.SessionID, "pane-1", "workspace-1"); err != nil {
		t.Fatal(err)
	}
	intents, err = scanner.ReviveIntents()
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].State != ReviveAttached || intents[0].PaneID != "pane-1" || intents[0].WorkspaceID != "workspace-1" {
		t.Fatalf("unexpected attached intent: %#v", intents)
	}
}

func writeReviveSession(t *testing.T, root, sessionID, cwd string) {
	t.Helper()
	project := filepath.Join(root, "projects", "-tmp-revive")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, sessionID+".jsonl")
	data := `{"sessionId":"` + sessionID + `","cwd":"` + cwd + `","timestamp":"2026-08-02T12:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
