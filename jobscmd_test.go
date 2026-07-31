package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func writeCommandState(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/state.json"
	st := store.NewJSONStore(path)
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{
		{ID: "abcdefgh-1", PaneID: "w1:p1", TerminalID: "term-1", State: store.StateWaiting, ResetAtUTC: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC), ResumeAtUTC: time.Date(2026, 7, 31, 12, 1, 0, 0, time.UTC), Attempts: 0},
		{ID: "ijklmnop-2", PaneID: "w1:p2", State: store.StateResumed, ResetAtUTC: time.Date(2026, 7, 31, 11, 0, 0, 0, time.UTC), ResumeAtUTC: time.Date(2026, 7, 31, 11, 1, 0, 0, time.UTC), Attempts: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStatusCommandGoldenShape(t *testing.T) {
	path := writeCommandState(t)
	var out, errOut bytes.Buffer
	if got := jobCommand([]string{"status", "--state-file", path}, &out, &errOut); got != 0 {
		t.Fatalf("jobCommand status exit = %d, stderr=%q", got, errOut.String())
	}
	wantHeader := "JOB       PANE   STATE    RESET(local)"
	if !strings.Contains(out.String(), wantHeader) || !strings.Contains(out.String(), "abcdefgh") || !strings.Contains(out.String(), "w1:p1") {
		t.Fatalf("status output = %q", out.String())
	}
}

func TestInspectUniquePrefixAndAmbiguity(t *testing.T) {
	path := writeCommandState(t)
	var out, errOut bytes.Buffer
	if got := jobCommand([]string{"inspect", "abcdefgh", "--state-file", path}, &out, &errOut); got != 0 {
		t.Fatalf("inspect exit = %d, stderr=%q", got, errOut.String())
	}
	var decoded store.Job
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("inspect JSON error = %v; output=%q", err, out.String())
	}
	if decoded.ID != "abcdefgh-1" {
		t.Fatalf("inspected job = %#v", decoded)
	}
	if decoded.TerminalID != "term-1" || !strings.Contains(out.String(), `"terminal_id": "term-1"`) {
		t.Fatalf("inspect output = %q, decoded = %#v", out.String(), decoded)
	}
	ambiguous := t.TempDir() + "/ambiguous.json"
	if err := store.NewJSONStore(ambiguous).Save(store.File{Version: 1, Jobs: []store.Job{{ID: "same-a"}, {ID: "same-b"}}}); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if got := jobCommand([]string{"inspect", "same", "--state-file", ambiguous}, &out, &errOut); got == 0 || !strings.Contains(errOut.String(), "ambiguous") {
		t.Fatalf("ambiguous inspect exit=%d stderr=%q", got, errOut.String())
	}
}

func TestCancelRoundTripAndTerminalRejection(t *testing.T) {
	path := writeCommandState(t)
	var out, errOut bytes.Buffer
	if got := jobCommand([]string{"cancel", "abcdefgh", "--state-file", path}, &out, &errOut); got != 0 {
		t.Fatalf("cancel exit = %d, stderr=%q", got, errOut.String())
	}
	file, err := store.NewJSONStore(path).Load()
	if err != nil || file.Jobs[0].State != store.StateCancelled {
		t.Fatalf("cancelled file = %#v, err=%v", file, err)
	}
	errOut.Reset()
	if got := jobCommand([]string{"cancel", "ijklmnop", "--state-file", path}, &out, &errOut); got == 0 || !strings.Contains(errOut.String(), "terminal") {
		t.Fatalf("terminal cancel exit=%d stderr=%q", got, errOut.String())
	}
}

func TestCLIJobDispatch(t *testing.T) {
	path := writeCommandState(t)
	var out, errOut bytes.Buffer
	if got := runCLI([]string{"status", "--state-file", path}, &out, &errOut); got != 0 {
		t.Fatalf("runCLI status exit = %d, stderr=%q", got, errOut.String())
	}
}

func TestJobCommandUsesStateFileFromConfigWhenFlagUnset(t *testing.T) {
	statePath := writeCommandState(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nstate:\n  file: "+statePath+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if got := jobCommand([]string{"status", "--config", configPath}, &out, &errOut); got != 0 {
		t.Fatalf("jobCommand status exit = %d, stderr=%q", got, errOut.String())
	}
	if !strings.Contains(out.String(), "abcdefgh") {
		t.Fatalf("status output = %q, want configured state file", out.String())
	}
}
