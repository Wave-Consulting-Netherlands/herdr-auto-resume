package sessionfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

var scannerNow = time.Date(2026, 8, 1, 16, 0, 0, 0, time.UTC)

func TestScanRealClaudeFixturesProducesBothObservations(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	for _, fixture := range []struct {
		project string
		file    string
	}{
		{"-home-ubuntu-dev-psft-run-script", "ce7bb791-f92c-4edb-b795-08a4fff2b778.jsonl"},
		{"-home-ubuntu-dev-Herdr-auto-resume", "829d1239-1560-476e-8744-e0c4df8b2b8c.jsonl"},
	} {
		dir := filepath.Join(projects, fixture.project)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join("testdata", fixture.file))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fixture.file), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	scanner, err := New(Config{RootDir: root, StatePath: filepath.Join(root, "state.json"), Now: func() time.Time { return scannerNow }})
	if err != nil {
		t.Fatal(err)
	}
	got, err := scanner.Scan()
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].SessionID < got[j].SessionID })
	if len(got) != 2 {
		t.Fatalf("observations = %#v, want both fixtures", got)
	}
	wantReset := time.Date(2026, 8, 1, 16, 30, 0, 0, time.UTC)
	if got[0].SessionID != "829d1239-1560-476e-8744-e0c4df8b2b8c" || got[0].CWD != "/home/ubuntu/dev/Herdr-auto-resume" || !got[0].ResetAt.Equal(wantReset) {
		t.Errorf("first observation = %#v", got[0])
	}
	if got[1].SessionID != "ce7bb791-f92c-4edb-b795-08a4fff2b778" || got[1].CWD != "/home/ubuntu/dev/psft_run_script" || !got[1].ResetAt.Equal(wantReset) {
		t.Errorf("second observation = %#v", got[1])
	}
}

func TestScanStrictlyDiscoversTopLevelUUIDFilesAndRejectsSidechains(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "projects", "project")
	if err := os.MkdirAll(filepath.Join(project, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	valid := mustRecord(t, "11111111-1111-4111-8111-111111111111", "req-valid", false)
	writeRecord(t, filepath.Join(project, "11111111-1111-4111-8111-111111111111.jsonl"), valid)
	writeRecord(t, filepath.Join(project, "not-a-session.jsonl"), valid)
	writeRecord(t, filepath.Join(project, "nested", "22222222-2222-4222-8222-222222222222.jsonl"), valid)
	writeRecord(t, filepath.Join(project, "22222222-2222-4222-8222-222222222222.jsonl"), mustRecord(t, "22222222-2222-4222-8222-222222222222", "req-sidechain", true))

	scanner := mustScanner(t, root)
	got, err := scanner.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SessionID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("observations = %#v, want only valid top-level non-sidechain record", got)
	}
}

func TestScanHandlesPartialLinesAcrossScans(t *testing.T) {
	root := t.TempDir()
	path := sessionPath(t, root, "33333333-3333-4333-8333-333333333333")
	first := mustRecord(t, "33333333-3333-4333-8333-333333333333", "req-first", false)
	second := mustRecord(t, "33333333-3333-4333-8333-333333333333", "req-second", false)
	if err := os.WriteFile(path, append(first, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	partial := second[:len(second)/2]
	if file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0); err != nil {
		t.Fatal(err)
	} else {
		_, _ = file.Write(partial)
		_ = file.Close()
	}
	scanner := mustScanner(t, root)
	got, err := scanner.Scan()
	if err != nil || len(got) != 1 {
		t.Fatalf("first scan = %#v, %v", got, err)
	}
	if file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0); err != nil {
		t.Fatal(err)
	} else {
		_, _ = file.Write(append(second[len(second)/2:], '\n'))
		_ = file.Close()
	}
	got, err = scanner.Scan()
	if err != nil || len(got) != 1 || got[0].RequestID != "req-second" {
		t.Fatalf("second scan = %#v, %v", got, err)
	}
}

func TestScanHandlesTruncationAndInodeReplacement(t *testing.T) {
	root := t.TempDir()
	path := sessionPath(t, root, "44444444-4444-4444-8444-444444444444")
	first := mustRecord(t, "44444444-4444-4444-8444-444444444444", "req-first", false)
	second := mustRecord(t, "44444444-4444-4444-8444-444444444444", "req-second", false)
	writeRecord(t, path, first)
	scanner := mustScanner(t, root)
	if got, err := scanner.Scan(); err != nil || len(got) != 1 {
		t.Fatalf("initial scan = %#v, %v", got, err)
	}
	writeRecord(t, path, second)
	got, err := scanner.Scan()
	if err != nil || len(got) != 1 || got[0].RequestID != "req-second" {
		t.Fatalf("truncated scan = %#v, %v", got, err)
	}
	oldPath := path + ".old"
	if err := os.Rename(path, oldPath); err != nil {
		t.Fatal(err)
	}
	third := mustRecord(t, "44444444-4444-4444-8444-444444444444", "req-third", false)
	writeRecord(t, path, third)
	got, err = scanner.Scan()
	if err != nil || len(got) != 1 || got[0].RequestID != "req-third" {
		t.Fatalf("replacement scan = %#v, %v", got, err)
	}
}

func TestScanDeduplicatesRequestIDsAndWritesSecureVersionedSidecar(t *testing.T) {
	root := t.TempDir()
	path := sessionPath(t, root, "55555555-5555-4555-8555-555555555555")
	record := mustRecord(t, "55555555-5555-4555-8555-555555555555", "req-same", false)
	writeRecord(t, path, append(append([]byte(nil), record...), append([]byte("\n"), append(record, '\n')...)...))
	state := filepath.Join(root, "state.json")
	scanner := mustScannerWithState(t, root, state)
	got, err := scanner.Scan()
	if err != nil || len(got) != 1 {
		t.Fatalf("scan = %#v, %v", got, err)
	}
	got, err = scanner.Scan()
	if err != nil || len(got) != 0 {
		t.Fatalf("repeat scan = %#v, %v", got, err)
	}
	sidecar := state + ".scan.json"
	info, err := os.Stat(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("sidecar mode = %o, want 600", info.Mode().Perm())
	}
	var saved struct {
		Version int                  `json:"version"`
		Pending []SessionObservation `json:"pending"`
	}
	data, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Version != 1 || len(saved.Pending) != 1 || saved.Pending[0].RequestID != "req-same" {
		t.Fatalf("sidecar = %#v, want version 1 and one pending observation", saved)
	}
}

func TestScanCrashAfterPendingPersistAdvancesCursorOnRetry(t *testing.T) {
	root := t.TempDir()
	path := sessionPath(t, root, "66666666-6666-4666-8666-666666666666")
	writeRecord(t, path, mustRecord(t, "66666666-6666-4666-8666-666666666666", "req-crash", false))
	scanner := mustScanner(t, root)
	wantCrash := errors.New("simulated crash after durable observation")
	scanner.afterPersist = func(SessionObservation) error { return wantCrash }
	if _, err := scanner.Scan(); !errors.Is(err, wantCrash) {
		t.Fatalf("crash scan error = %v, want simulated crash", err)
	}
	state, err := scanner.readSidecar()
	if err != nil {
		t.Fatal(err)
	}
	cursor := state.Files[path]
	if len(state.Pending) != 1 || !state.SeenRequestIDs["req-crash"] || cursor.Offset != 0 {
		t.Fatalf("post-crash sidecar = %#v, cursor = %#v; want pending without cursor advance", state, cursor)
	}
	scanner.afterPersist = nil
	got, err := scanner.Scan()
	if err != nil || len(got) != 0 {
		t.Fatalf("retry scan = %#v, %v; persisted request must deduplicate", got, err)
	}
	state, err = scanner.readSidecar()
	if err != nil {
		t.Fatal(err)
	}
	if state.Files[path].Offset == 0 {
		t.Fatal("retry did not advance cursor after observing the already-persisted request")
	}
}

func mustScanner(t *testing.T, root string) *Scanner {
	t.Helper()
	return mustScannerWithState(t, root, filepath.Join(root, "state.json"))
}

func mustScannerWithState(t *testing.T, root, state string) *Scanner {
	t.Helper()
	scanner, err := New(Config{RootDir: root, StatePath: state, Lookback: 2 * time.Hour, Now: func() time.Time { return scannerNow }})
	if err != nil {
		t.Fatal(err)
	}
	return scanner
}

func sessionPath(t *testing.T, root, sessionID string) string {
	t.Helper()
	dir := filepath.Join(root, "projects", "project")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, sessionID+".jsonl")
}

func writeRecord(t *testing.T, path string, data []byte) {
	t.Helper()
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustRecord(t *testing.T, sessionID, requestID string, sidechain bool) []byte {
	t.Helper()
	record := map[string]any{
		"isSidechain": sidechain,
		"type":        "assistant",
		"timestamp":   "2026-08-01T15:30:00Z",
		"message":     map[string]any{"content": []any{map[string]any{"type": "text", "text": "You've hit your session limit · resets 4:30pm (UTC)"}}},
		"requestId":   requestID,
		"error":       "rate_limit",
		"cwd":         "/tmp/project",
		"sessionId":   sessionID,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
