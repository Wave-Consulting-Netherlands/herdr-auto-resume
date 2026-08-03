package jobs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/sessionfile"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func TestManagerDeduplicatesScrapeAndFileEventsBySidecarEpisode(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	registry, err := sessionfile.NewEpisodeRegistry(state)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "11111111-1111-4111-8111-111111111111"
	pane := runtime.Pane{ID: "p1", Agent: "claude", AgentSessionID: sessionID}
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{pane}}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	m.episodeRegistry = registry
	reset := testNow.Add(time.Hour)
	scrape := LimitEvent{Pane: pane, Provider: "claude", ResetsRaw: "5m", ResetTime: reset, Spec: detection.ResetSpec{Kind: detection.ResetKindRelative, ParsedTime: reset}, Evidence: "same evidence", Source: "scrape", ObservedAt: testNow}
	file := scrape
	file.Provider = "claude"
	file.Source = "session-file"
	file.Pane.ID = "p2"
	file.ResetTime = reset.Add(3 * time.Minute)
	if !m.HandleLimit(scrape) {
		t.Fatal("scrape event was not accepted")
	}
	if !m.HandleLimit(file) {
		t.Fatal("delayed file event did not deduplicate as owned")
	}
	file.EpisodeID = m.Snapshot()[0].Episode
	if !m.HandleLimit(file) {
		t.Fatal("pre-resolved delayed file event did not deduplicate as owned")
	}
	jobs := m.Snapshot()
	if len(jobs) != 1 || jobs[0].Episode == "" || jobs[0].Source != "scrape" {
		t.Fatalf("jobs = %#v, want one scrape-sourced episode job", jobs)
	}
}

func TestManagerKeepsLegacyPaneEvidenceFallbackWithoutSessionIdentity(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	registry, err := sessionfile.NewEpisodeRegistry(state)
	if err != nil {
		t.Fatal(err)
	}
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "claude"}}}}
	m, _ := newTestManager(t, rt, Config{Margin: time.Minute}, "job-1")
	m.episodeRegistry = registry
	event := limitEvent(limitedContent(), testNow.Add(time.Hour))
	event.Evidence = "legacy evidence"
	event.Source = "scrape"
	if !m.HandleLimit(event) || !m.HandleLimit(event) {
		t.Fatal("legacy pane/evidence event should remain owned on repeated polls")
	}
	if len(m.Snapshot()) != 1 || m.Snapshot()[0].Episode != "" {
		t.Fatalf("jobs = %#v, want one legacy fallback job without episode identity", m.Snapshot())
	}
}

func TestPendingObservationAndJobConvergeAfterCrashBeforeSidecarCommit(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	sessionID := "22222222-2222-4222-8222-222222222222"
	project := filepath.Join(root, "projects", "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(map[string]any{
		"isSidechain": false, "timestamp": "2026-08-01T15:30:00Z", "sessionId": sessionID,
		"cwd": "/work", "requestId": "pending-request", "error": "rate_limit",
		"message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "You've hit your session limit · resets 4:30pm (UTC)"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, sessionID+".jsonl"), append(record, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner, err := sessionfile.New(sessionfile.Config{RootDir: root, StatePath: statePath, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	observations, err := scanner.Scan()
	if err != nil || len(observations) != 1 {
		t.Fatalf("scan = %#v, %v", observations, err)
	}
	registry, err := sessionfile.NewEpisodeRegistry(statePath)
	if err != nil {
		t.Fatal(err)
	}
	episode, duplicate, err := registry.Resolve(observations[0])
	if err != nil || duplicate {
		t.Fatalf("episode = %#v duplicate=%v err=%v", episode, duplicate, err)
	}
	rt := &testRuntime{Fake: runtime.Fake{PanesList: []runtime.Pane{{ID: "p1", Agent: "claude", AgentSessionID: sessionID}}}}
	st := store.NewJSONStore(statePath)
	m := New(rt, st, Config{Margin: time.Minute}, WithClock(func() time.Time { return testNow }), WithIDGenerator(func() string { return "job-1" }), WithEpisodeRegistry(registry))
	event := LimitEvent{Pane: rt.PanesList[0], Provider: "claude", EpisodeID: episode.ID, Source: "session-file", ResetsRaw: observations[0].ResetRaw, ResetTime: observations[0].ResetAt, ObservedAt: observations[0].ObservedAt}
	if !m.HandleLimit(event) || len(m.Snapshot()) != 1 {
		t.Fatalf("job creation = %#v, want one job before simulated commit seam", m.Snapshot())
	}
	if err := scanner.ReconcilePending(m.EpisodeIDs()); err != nil {
		t.Fatal(err)
	}
	pending, err := scanner.Pending()
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after startup reconciliation = %#v, %v; want committed", pending, err)
	}
}
