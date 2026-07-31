package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/coordinator"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

type blockingListRuntime struct {
	runtime.Fake
	started chan struct{}
	resume  chan struct{}
}

func (r *blockingListRuntime) ListPanes() ([]runtime.Pane, error) {
	select {
	case r.started <- struct{}{}:
	default:
	}
	select {
	case <-r.resume:
	case <-context.Background().Done():
	}
	return r.Fake.ListPanes()
}

func TestReconcileCorruptStoreWarnsAndContinues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	m := New(&testRuntime{}, st, Config{}, WithClock(func() time.Time { return testNow }))
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error = %v, want warning and continuation", err)
	}
	if len(m.Snapshot()) != 0 {
		t.Fatalf("jobs after corrupt recovery = %#v, want empty", m.Snapshot())
	}
}

func TestReconcileKeepsWaitingAndValidatingButResetsUncertainResuming(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	jobs := []store.Job{
		{ID: "waiting", PaneID: "p1", State: store.StateWaiting, ResumeAtUTC: testNow.Add(time.Hour)},
		{ID: "validating", PaneID: "p2", State: store.StateValidating, ResumeAtUTC: testNow},
		{ID: "resuming", PaneID: "p3", State: store.StateResuming, ResumeAtUTC: testNow},
		{ID: "verifying", PaneID: "p4", State: store.StateVerifyingResume, ResumeAtUTC: testNow, VerifyDeadlineUTC: testNow.Add(time.Minute)},
	}
	if err := st.Save(store.File{Version: 1, Jobs: jobs}); err != nil {
		t.Fatal(err)
	}
	m := New(&testRuntime{}, st, Config{MaxHorizon: 192 * time.Hour}, WithClock(func() time.Time { return testNow }))
	if err := m.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	got := m.Snapshot()
	if got[0].State != store.StateWaiting || got[1].State != store.StateValidating || got[2].State != store.StateManualRequired || got[3].State != store.StateVerifyingResume {
		t.Fatalf("reconciled jobs = %#v", got)
	}
}

func TestReconcileFailsWaitingBeyondHorizon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	job := store.Job{ID: "late", PaneID: "p1", State: store.StateWaiting, ResumeAtUTC: testNow.Add(2 * time.Hour)}
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{job}}); err != nil {
		t.Fatal(err)
	}
	m := New(&testRuntime{}, st, Config{MaxHorizon: time.Hour}, WithClock(func() time.Time { return testNow }))
	if err := m.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	got := m.Snapshot()[0]
	if got.State != store.StateFailed || got.LastError == "" {
		t.Fatalf("job = %#v, want failed horizon job", got)
	}
}

func TestRestartE2EThroughCoordinatorPersistsWaitAndSendsOnce(t *testing.T) {
	content := "You've hit your limit · resets 5m"
	detectionNow := testNow
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	rt := &testRuntime{Fake: runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}},
		Content:   map[string]string{"p1": content},
		Procs:     map[string]runtime.ProcessInfo{"p1": {Command: "claude", CWD: "/work"}},
	}}
	clockA := detectionNow
	mA := New(rt, st, Config{Margin: time.Minute}, WithClock(func() time.Time { return clockA }), WithSleep(func(time.Duration) {}), WithIDGenerator(func() string { return "job-1" }))
	cA := coordinator.New(rt, coordinator.Config{ReadLines: 20}, coordinator.WithClock(func() time.Time { return clockA }), coordinator.WithJobSink(mA), coordinator.WithSleep(func(time.Duration) {}))
	cA.SetPanes(rt.PanesList)
	cA.Poll()
	cA.EnableAll()
	cA.Poll()
	first, err := st.Load()
	if err != nil || len(first.Jobs) != 1 || first.Jobs[0].State != store.StateWaiting {
		t.Fatalf("instance A state = %#v, err=%v, want WAITING", first, err)
	}
	if len(rt.SentText) != 0 {
		t.Fatalf("instance A sent %d inputs while waiting", len(rt.SentText))
	}

	clockB := clockA
	mB := New(rt, st, Config{Margin: time.Minute}, WithClock(func() time.Time { return clockB }), WithSleep(func(time.Duration) {}), WithIDGenerator(func() string { return "unused" }))
	if err := mB.Reconcile(); err != nil {
		t.Fatalf("instance B Reconcile() error = %v", err)
	}
	cB := coordinator.New(rt, coordinator.Config{ReadLines: 20}, coordinator.WithClock(func() time.Time { return clockB }), coordinator.WithJobSink(mB), coordinator.WithSleep(func(time.Duration) {}))
	cB.SetPanes(rt.PanesList)
	cB.EnableAll()
	cB.Poll()
	resumedAt := first.Jobs[0].ResumeAtUTC
	clockB = resumedAt
	mB.Tick(clockB)
	if len(rt.SentText) != 1 {
		t.Fatalf("total sends after restart = %d, want 1", len(rt.SentText))
	}
	rt.Content["p1"] = content + "\ncontinued output"
	mB.Tick(clockB.Add(time.Second))
	if got := mB.Snapshot()[0].State; got != store.StateResumed {
		t.Fatalf("instance B final state = %s, want RESUMED", got)
	}
	if len(rt.SentText) != 1 {
		t.Fatalf("total sends after verification = %d, want 1", len(rt.SentText))
	}
}

func TestReconcileResumingRequiresManualWithoutSending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{{ID: "job-1", PaneID: "p1", State: store.StateResuming, ResumeAtUTC: testNow}}}); err != nil {
		t.Fatal(err)
	}
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": limitedContent()}}}
	m := New(rt, st, Config{}, WithClock(func() time.Time { return testNow }), WithSleep(func(time.Duration) {}))
	if err := m.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	m.Tick(testNow.Add(time.Hour))
	if got := m.Snapshot()[0].State; got != store.StateManualRequired {
		t.Fatalf("state = %s, want MANUAL_REQUIRED", got)
	}
	if len(rt.SentText) != 0 || len(rt.SentKeys) != 0 {
		t.Fatalf("runtime writes = %#v/%#v, want none", rt.SentText, rt.SentKeys)
	}
}

func TestTickReloadsExternallyCancelledJobByMTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1"}}, Content: map[string]string{"p1": limitedContent()}}}
	m := New(rt, st, Config{}, WithClock(func() time.Time { return testNow }))
	job := store.Job{ID: "job-1", PaneID: "p1", State: store.StateWaiting, ResumeAtUTC: testNow}
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{job}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Reconcile(); err != nil {
		t.Fatal(err)
	}
	job.State = store.StateCancelled
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{job}}); err != nil {
		t.Fatal(err)
	}
	m.Tick(testNow.Add(time.Hour))
	if got := m.Snapshot()[0].State; got != store.StateCancelled {
		t.Fatalf("state = %s, want externally cancelled state", got)
	}
	if len(rt.SentText) != 0 {
		t.Fatal("externally cancelled job sent input")
	}
}

func TestTwoStoreHandleCancelRaceNeverSends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st := store.NewJSONStore(path)
	job := store.Job{ID: "job-1", Provider: "claude", Agent: "claude", PaneID: "p1", State: store.StateWaiting, ResumeAtUTC: testNow}
	if err := st.Save(store.File{Version: 1, Jobs: []store.Job{job}}); err != nil {
		t.Fatal(err)
	}
	rt := &blockingListRuntime{
		Fake:    runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}}, Content: map[string]string{"p1": "╭────╮\n> "}, Procs: map[string]runtime.ProcessInfo{"p1": {Command: "claude"}}},
		started: make(chan struct{}, 1),
		resume:  make(chan struct{}),
	}
	m := New(rt, st, Config{Provider: "claude", Margin: time.Minute}, WithProviders(providerRegistryForTest()), WithClock(func() time.Time { return testNow }), WithSleep(func(time.Duration) {}))
	done := make(chan struct{})
	go func() {
		m.Tick(testNow)
		close(done)
	}()
	<-rt.started

	if err := store.WithLock(store.NewJSONStore(path), func() error {
		fresh, err := store.NewJSONStore(path).Load()
		if err != nil {
			return err
		}
		fresh.Jobs[0].State = store.StateCancelled
		fresh.Jobs[0].LastValidation = "cancelled by user"
		return store.NewJSONStore(path).Save(fresh)
	}); err != nil {
		t.Fatalf("cancel transaction: %v", err)
	}
	close(rt.resume)
	<-done
	loaded, err := store.NewJSONStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Jobs[0].State; got != store.StateCancelled {
		t.Fatalf("final state = %s, want CANCELLED", got)
	}
	if len(rt.SentText) != 0 || len(rt.SentKeys) != 0 {
		t.Fatalf("runtime writes = text %#v keys %#v, want none", rt.SentText, rt.SentKeys)
	}
}

func TestStartupBannerIsCapturedBeforeLongCadenceTick(t *testing.T) {
	content := limitedContent()
	rt := &testRuntime{Fake: runtime.Fake{
		PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}},
		Content:   map[string]string{"p1": content},
	}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	c := coordinator.New(rt, coordinator.Config{ReadLines: 20}, coordinator.WithJobSink(m), coordinator.WithClock(func() time.Time { return testNow }))
	c.SetPanes(rt.PanesList)
	c.Poll()
	c.EnableAll()
	// This is the immediate action-capable startup poll; the long interval has
	// not fired yet.
	c.Poll()
	rt.Content["p1"] = readyContent()
	if got := len(m.Snapshot()); got != 1 {
		t.Fatalf("jobs after startup banner disappeared = %d, want one", got)
	}
}

func providerRegistryForTest() *provider.Registry {
	return provider.NewRegistry(claude.New(""))
}
