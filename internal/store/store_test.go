package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestJobStateTerminal(t *testing.T) {
	for _, state := range []JobState{StateResumed, StateManualRequired, StateFailed, StateCancelled, StateDisabled, StateSessionGone} {
		if !state.Terminal() {
			t.Errorf("%s.Terminal() = false, want true", state)
		}
	}
	for _, state := range []JobState{StateWaiting, StateValidating, StateResuming, StateVerifyingResume} {
		if state.Terminal() {
			t.Errorf("%s.Terminal() = true, want false", state)
		}
	}
}

func TestJSONStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	s := NewJSONStore(path)
	want := File{
		Version: 1,
		Jobs: []Job{{
			ID:                "job-1",
			Provider:          "claude",
			PaneID:            "w1:p1",
			Workspace:         "workspace",
			Agent:             "claude-code",
			ProcCommand:       "claude --resume",
			WorkingDir:        "/work",
			DetectedAt:        time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC),
			RawReset:          "5m",
			ResetAtUTC:        time.Date(2026, 7, 31, 1, 7, 3, 0, time.UTC),
			ResumeAtUTC:       time.Date(2026, 7, 31, 1, 8, 3, 0, time.UTC),
			MarginSecs:        60,
			State:             StateWaiting,
			Attempts:          0,
			AttemptID:         "",
			AttemptAtUTC:      time.Time{},
			VerifyDeadlineUTC: time.Time{},
			LastValidation:    "",
			LastError:         "",
			EvidenceHash:      "hash",
			EvidenceAtUTC:     time.Date(2026, 7, 31, 1, 2, 4, 0, time.UTC),
			DryRun:            true,
		}},
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !equalJSON(want, got) {
		t.Fatalf("round trip mismatch:\nwant %#v\ngot  %#v", want, got)
	}
}

func TestJSONStoreMissingFileReturnsEmptyVersionOne(t *testing.T) {
	s := NewJSONStore(filepath.Join(t.TempDir(), "state.json"))
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Version != 1 || len(got.Jobs) != 0 {
		t.Fatalf("Load() = %#v, want empty version 1 file", got)
	}
}

func TestJSONStoreLoadsVersionOneWithoutCoercion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"jobs":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := NewJSONStore(path).Load()
	if err != nil || got.Version != 1 || len(got.Jobs) != 0 {
		t.Fatalf("Load() = %#v, err=%v; want schema version 1", got, err)
	}
}

func TestJSONStoreCoercesUnversionedStateToVersionOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":0,"jobs":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := NewJSONStore(path).Load()
	if err != nil || got.Version != 1 || len(got.Jobs) != 0 {
		t.Fatalf("Load() = %#v, err=%v; want version-0 input coerced to 1", got, err)
	}
}

func TestJSONStoreRejectsFutureSchemaVersion(t *testing.T) {
	for _, version := range []int{2, 99} {
		t.Run(fmt.Sprintf("version-%d", version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"version":%d,"jobs":[]}`, version)), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := NewJSONStore(path).Load()
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("version %d", version)) || !strings.Contains(err.Error(), "supported version 1") {
				t.Fatalf("Load() error = %v, want found and supported versions", err)
			}
			if _, statErr := os.Stat(path); statErr != nil {
				t.Fatalf("future-version state file was altered or removed: %v", statErr)
			}
		})
	}
}

func TestJSONStoreCorruptBacksUpAndRecovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewJSONStore(path)
	got, err := s.Load()
	if got.Version != 1 || len(got.Jobs) != 0 {
		t.Fatalf("recovered file = %#v, want empty version 1", got)
	}
	var corrupt CorruptError
	if !errors.As(err, &corrupt) {
		t.Fatalf("Load() error = %v, want CorruptError", err)
	}
	if corrupt.BackupPath == "" {
		t.Fatal("CorruptError.BackupPath is empty")
	}
	backup, readErr := os.ReadFile(corrupt.BackupPath)
	if readErr != nil || string(backup) != "not json" {
		t.Fatalf("backup = %q, read error = %v", backup, readErr)
	}
	recovered, readErr := s.Load()
	if readErr != nil {
		t.Fatalf("Load() after recovery error = %v", readErr)
	}
	if recovered.Version != 1 || len(recovered.Jobs) != 0 {
		t.Fatalf("recovered second load = %#v", recovered)
	}
}

func TestJSONStoreSaveIsAtomicAndCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := NewJSONStore(path)
	if err := s.Save(File{Version: 1}); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}
	if err := s.Save(File{Version: 1, Jobs: []Job{{ID: "new"}}}); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "state.json.tmp.") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
	var decoded File
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("state.json is not valid JSON: %v", err)
	}
	if len(decoded.Jobs) != 1 || decoded.Jobs[0].ID != "new" {
		t.Fatalf("state.json = %#v", decoded)
	}
}

func TestJSONStoreToleratesUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := []byte(`{"version":1,"jobs":[],"future":"ignored"}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := NewJSONStore(path).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Version != 1 || len(got.Jobs) != 0 {
		t.Fatalf("Load() = %#v", got)
	}
}

func TestJSONStoreLoadsSchemaOneFileWithoutPhaseFourFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := []byte(`{"version":1,"jobs":[{"id":"old","provider":"claude","pane_id":"w1:p1","state":"WAITING","reset_at_utc":"2026-07-31T01:00:00Z"}]}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := NewJSONStore(path).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Version != 1 || len(got.Jobs) != 1 || got.Jobs[0].TerminalID != "" || got.Jobs[0].ResetKind != "" || got.Jobs[0].ResetTimezone != "" || got.Jobs[0].Confidence != "" {
		t.Fatalf("old file loaded as %#v, want schema-1 job with empty additive fields", got)
	}
	if err := NewJSONStore(path).Save(got); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := NewJSONStore(path).Load()
	if err != nil || roundTrip.Version != 1 || roundTrip.Jobs[0].TerminalID != "" {
		t.Fatalf("old file round trip = %#v, err=%v", roundTrip, err)
	}
}

func TestDefaultPathHonorsXDGStateHome(t *testing.T) {
	stateHome := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", stateHome)
	if got, want := DefaultPath(), filepath.Join(stateHome, "herdr-auto-resume", "state.json"); got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestJSONStorePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "state.json")
	if err := NewJSONStore(path).Save(File{Version: 1}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0700 {
		t.Fatalf("state directory permissions = %04o, want 0700", mode)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0600 {
		t.Fatalf("state file permissions = %04o, want 0600", mode)
	}
}

func TestJSONStoreSaveIntoPreExistingUnownedDirectoryKeepsFileSecure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root owns /tmp and could change its mode")
	}
	info, err := os.Stat("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint32(os.Getuid()) == stat.Uid {
		t.Skip("/tmp is owned by the test user")
	}

	path := filepath.Join("/tmp", fmt.Sprintf("herdr-json-store-%d-%d.json", os.Getpid(), time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(path) })
	if err := NewJSONStore(path).Save(File{Version: 1}); err != nil {
		t.Fatalf("Save() into pre-existing unowned directory error = %v", err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fileInfo.Mode().Perm(); mode != 0600 {
		t.Fatalf("state file permissions = %04o, want 0600", mode)
	}
}

func equalJSON(a, b File) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
