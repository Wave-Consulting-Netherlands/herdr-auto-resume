package jobs

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func TestHandleLimitStampsTerminalIdentityAndWorkspace(t *testing.T) {
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", TerminalID: "term-1", WorkspaceID: "w1"}}}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	event := limitEvent(limitedContent(), testNow.Add(time.Hour))
	event.Pane.TerminalID = "term-1"
	event.Pane.WorkspaceID = "w1"
	if !m.HandleLimit(event) {
		t.Fatal("HandleLimit() = false")
	}
	job := m.Snapshot()[0]
	if job.TerminalID != "term-1" || job.Workspace != "w1" {
		t.Fatalf("job identity = %#v, want terminal/workspace stamped", job)
	}
}

func TestReassignPaneUsesFreshFlockTransactionAndMatchingTerminalID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{{ID: "job-1", PaneID: "old", TerminalID: "term-1", Workspace: "w1", State: store.StateWaiting}}}); err != nil {
		t.Fatal(err)
	}
	m := New(&testRuntime{}, st, Config{})
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{{ID: "job-1", PaneID: "old", TerminalID: "term-1", Workspace: "w1", State: store.StateWaiting}}}); err != nil {
		t.Fatal(err)
	}
	if err := m.ReassignPane("old", runtime.Pane{ID: "new", TerminalID: "term-1", WorkspaceID: "w2"}); err != nil {
		t.Fatalf("ReassignPane(): %v", err)
	}
	got, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Jobs[0].PaneID != "new" || got.Jobs[0].Workspace != "w2" || got.Jobs[0].TerminalID != "term-1" {
		t.Fatalf("stored job = %#v", got.Jobs[0])
	}
}

func TestReassignPaneMismatchRequiresManualAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{{ID: "job-1", PaneID: "old", TerminalID: "term-1", State: store.StateWaiting}}}); err != nil {
		t.Fatal(err)
	}
	m := New(&testRuntime{}, st, Config{})
	if err := m.ReassignPane("old", runtime.Pane{ID: "new", TerminalID: "term-2"}); err != nil {
		t.Fatal(err)
	}
	job := m.Snapshot()[0]
	if job.State != store.StateManualRequired || !strings.Contains(job.LastError, "pane identity changed") {
		t.Fatalf("job = %#v, want identity mismatch manual-required", job)
	}
}

func TestReassignPaneLegacyUniqueUpdatesAndAmbiguousRequiresManual(t *testing.T) {
	t.Run("unique", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		st := store.NewJSONStore(path)
		_ = st.Save(store.File{Version: 1, Jobs: []store.Job{{ID: "job-1", PaneID: "old", State: store.StateWaiting}}})
		m := New(&testRuntime{}, st, Config{})
		if err := m.ReassignPane("old", runtime.Pane{ID: "new", TerminalID: "term-1"}); err != nil {
			t.Fatal(err)
		}
		if got := m.Snapshot()[0].PaneID; got != "new" {
			t.Fatalf("PaneID = %q, want new", got)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "state.json")
		st := store.NewJSONStore(path)
		_ = st.Save(store.File{Version: 1, Jobs: []store.Job{{ID: "job-a", PaneID: "old", State: store.StateWaiting}, {ID: "job-b", PaneID: "old", State: store.StateWaiting}}})
		m := New(&testRuntime{}, st, Config{})
		if err := m.ReassignPane("old", runtime.Pane{ID: "new", TerminalID: "term-1"}); err != nil {
			t.Fatal(err)
		}
		for _, job := range m.Snapshot() {
			if job.State != store.StateManualRequired {
				t.Fatalf("job = %#v, want manual-required", job)
			}
		}
	})
}

func TestReconcilePanesReassignsMissingPaneByTerminalID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{{ID: "job-1", PaneID: "old", TerminalID: "term-1", State: store.StateWaiting}}}); err != nil {
		t.Fatal(err)
	}
	m := New(&testRuntime{}, st, Config{})
	if err := m.ReconcilePanes([]runtime.Pane{{ID: "new", TerminalID: "term-1", WorkspaceID: "w2"}}); err != nil {
		t.Fatal(err)
	}
	job := m.Snapshot()[0]
	if job.PaneID != "new" || job.Workspace != "w2" {
		t.Fatalf("job = %#v, want pane and workspace reassigned", job)
	}
}

func TestReconcilePanesLeavesMissingOrAmbiguousIdentityForValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	jobs := []store.Job{{ID: "gone", PaneID: "old", TerminalID: "term-gone", State: store.StateWaiting}, {ID: "ambiguous", PaneID: "old2", TerminalID: "term-same", State: store.StateWaiting}}
	if err := st.Save(store.File{Version: 1, Jobs: jobs}); err != nil {
		t.Fatal(err)
	}
	m := New(&testRuntime{}, st, Config{})
	if err := m.ReconcilePanes([]runtime.Pane{{ID: "a", TerminalID: "term-same"}, {ID: "b", TerminalID: "term-same"}}); err != nil {
		t.Fatal(err)
	}
	got := m.Snapshot()
	if got[0].PaneID != "old" || got[1].PaneID != "old2" {
		t.Fatalf("jobs changed despite missing/ambiguous identity: %#v", got)
	}
}
