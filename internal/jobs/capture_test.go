package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func TestResumeCapturesBannerSafeToResumeContentWithPrivateModes(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}},
		Content:   map[string]string{"p1": content},
		Procs:     map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}},
	}}
	m, st := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	m.HandleLimit(limitEvent(content, testNow))
	m.Tick(testNow.Add(time.Minute))

	captureDir := filepath.Join(filepath.Dir(st.Path()), "captures")
	entries, err := os.ReadDir(captureDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("capture entries = %v, err=%v; want one capture", entries, err)
	}
	path := filepath.Join(captureDir, entries[0].Name())
	data, err := os.ReadFile(path)
	if err != nil || string(data) != content {
		t.Fatalf("capture = %q, err=%v; want SafeToResume content %q", data, err, content)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Fatalf("capture mode = %o, want 0600", mode)
	}
	if dirInfo, err := os.Stat(captureDir); err != nil {
		t.Fatalf("stat capture directory: %v", err)
	} else if dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("capture directory mode = %o, want 0700", dirInfo.Mode().Perm())
	}
}

func TestMenuAnswerCapturesGuardedPreAnswerContent(t *testing.T) {
	content := menuValidationContent(readMenuFixture(t, "claude/positive/cc2026-08_menu-team-plan.txt"))
	m, _, st := menuManager(t, content, true, false)
	if !m.HandleLimit(menuEvent(content)) {
		t.Fatal("HandleLimit() did not own menu episode")
	}
	m.Tick(testNow.Add(2 * time.Hour))

	captureDir := filepath.Join(filepath.Dir(st.Path()), "captures")
	entries, err := os.ReadDir(captureDir)
	if err != nil || len(entries) < 1 {
		t.Fatalf("capture entries = %v, err=%v; want menu capture", entries, err)
	}
	var found bool
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(captureDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) == content {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("captures in %s did not contain guarded menu content", captureDir)
	}
}

func TestCaptureFailureDoesNotBlockResume(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}},
		Content:   map[string]string{"p1": content},
		Procs:     map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}},
	}}
	m, st := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	capturePath := filepath.Join(filepath.Dir(st.Path()), "captures")
	if err := os.WriteFile(capturePath, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	m.HandleLimit(limitEvent(content, testNow))
	m.Tick(testNow.Add(time.Minute))

	job := m.Snapshot()[0]
	if job.State != store.StateVerifyingResume || len(rt.SentText) != 1 {
		t.Fatalf("job=%#v text=%#v, want resume despite capture failure", job, rt.SentText)
	}
}

func TestCaptureTruncatesIndividualFiles(t *testing.T) {
	m, st := newTestManager(t, &testRuntime{}, Config{}, "job-1")
	m.capturePaneContent("job-1", strings.Repeat("x", captureMaxFileBytes+1), testNow)

	captureDir := filepath.Join(filepath.Dir(st.Path()), "captures")
	entries, err := os.ReadDir(captureDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("capture entries = %v, err=%v; want one capture", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != captureMaxFileBytes {
		t.Fatalf("capture size = %d, want %d-byte cap", info.Size(), captureMaxFileBytes)
	}
}

func TestCaptureEvictsOldestWhenDirectoryExceedsBound(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i <= captureMaxDirBytes/captureMaxFileBytes; i++ {
		path := filepath.Join(dir, fmt.Sprintf("capture-%02d.txt", i))
		if err := os.WriteFile(path, []byte(strings.Repeat("x", captureMaxFileBytes)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := boundCaptures(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) >= captureMaxDirBytes/captureMaxFileBytes+1 {
		t.Fatalf("capture count = %d, want oldest eviction", len(entries))
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > captureMaxFileBytes {
			t.Fatalf("capture %s size=%d, want <= %d", entry.Name(), info.Size(), captureMaxFileBytes)
		}
	}
}
