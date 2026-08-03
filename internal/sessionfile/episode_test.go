package sessionfile

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func TestEpisodeRegistryDeduplicatesDelayedFileAfterScrape(t *testing.T) {
	registry := mustEpisodeRegistry(t)
	scrape := observation("scrape-request", time.Date(2026, 8, 1, 16, 30, 0, 0, time.UTC))
	file := observation("file-request", scrape.ResetAt.Add(3*time.Minute))
	first, duplicate, err := registry.Resolve(scrape)
	if err != nil || duplicate {
		t.Fatalf("scrape resolve = %#v, %v; want new episode", first, err)
	}
	second, duplicate, err := registry.Resolve(file)
	if err != nil || !duplicate || second.ID != first.ID || !second.FirstResetAt.Equal(first.FirstResetAt) {
		t.Fatalf("file resolve = %#v, duplicate=%v, err=%v; want same first-seen episode", second, duplicate, err)
	}
}

func TestEpisodeRegistryDeduplicatesScrapeAfterDelayedFile(t *testing.T) {
	registry := mustEpisodeRegistry(t)
	file := observation("file-request", time.Date(2026, 8, 1, 16, 30, 0, 0, time.UTC))
	scrape := observation("scrape-request", file.ResetAt.Add(-3*time.Minute))
	first, duplicate, err := registry.Resolve(file)
	if err != nil || duplicate {
		t.Fatalf("file resolve = %#v, %v; want new episode", first, err)
	}
	second, duplicate, err := registry.Resolve(scrape)
	if err != nil || !duplicate || second.ID != first.ID {
		t.Fatalf("scrape resolve = %#v, duplicate=%v, err=%v; want same episode", second, duplicate, err)
	}
}

func TestEpisodeRegistryUsesToleranceBoundaryAndNearestFirstSeen(t *testing.T) {
	registry := mustEpisodeRegistry(t)
	base := observation("base", time.Date(2026, 8, 1, 16, 30, 0, 0, time.UTC))
	first, duplicate, err := registry.Resolve(base)
	if err != nil || duplicate {
		t.Fatal("base should create an episode")
	}
	within := base
	within.RequestID = "within"
	within.ResetAt = base.ResetAt.Add(9*time.Minute + 59*time.Second)
	matched, duplicate, err := registry.Resolve(within)
	if err != nil || !duplicate || matched.ID != first.ID {
		t.Fatalf("9:59 resolve = %#v, duplicate=%v, err=%v; want match", matched, duplicate, err)
	}
	outside := base
	outside.RequestID = "outside"
	outside.ResetAt = base.ResetAt.Add(10*time.Minute + time.Second)
	newEpisode, duplicate, err := registry.Resolve(outside)
	if err != nil || duplicate || newEpisode.ID == first.ID {
		t.Fatalf("10:01 resolve = %#v, duplicate=%v, err=%v; want new episode", newEpisode, duplicate, err)
	}
}

func TestEpisodeRegistryRelativeObservationsUseResetTolerance(t *testing.T) {
	registry := mustEpisodeRegistry(t)
	first := observation("relative-a", time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	second := observation("relative-b", first.ResetAt.Add(3*time.Minute))
	a, duplicate, err := registry.Resolve(first)
	if err != nil || duplicate {
		t.Fatal("first relative observation should create an episode")
	}
	b, duplicate, err := registry.Resolve(second)
	if err != nil || !duplicate || a.ID != b.ID {
		t.Fatalf("relative resolve = %#v / %#v, duplicate=%v, err=%v; want match", a, b, duplicate, err)
	}
}

func TestEpisodeRegistrySurvivesV020JobRoundTripDroppingConvenienceEpisode(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	registry, err := NewEpisodeRegistry(state)
	if err != nil {
		t.Fatal(err)
	}
	obs := observation("request", time.Date(2026, 8, 1, 16, 30, 0, 0, time.UTC))
	episode, duplicate, err := registry.Resolve(obs)
	if err != nil || duplicate {
		t.Fatal("initial episode should be new")
	}
	job := store.Job{ID: "job-1", Episode: episode.ID, Source: "scrape"}
	oldWriter, err := json.Marshal(struct {
		ID string `json:"id"`
	}{ID: job.ID})
	if err != nil {
		t.Fatal(err)
	}
	var oldJob store.Job
	if err := json.Unmarshal(oldWriter, &oldJob); err != nil {
		t.Fatal(err)
	}
	if oldJob.Episode != "" {
		t.Fatal("v0.2.0-shaped round trip unexpectedly retained additive episode field")
	}
	again, duplicate, err := registry.Resolve(obs)
	if err != nil || !duplicate || again.ID != episode.ID {
		t.Fatalf("registry after old job round trip = %#v, duplicate=%v, err=%v; dedup must not depend on Job.Episode", again, duplicate, err)
	}
}

func mustEpisodeRegistry(t *testing.T) *EpisodeRegistry {
	t.Helper()
	registry, err := NewEpisodeRegistry(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func observation(requestID string, resetAt time.Time) SessionObservation {
	return SessionObservation{Provider: ProviderClaude, SessionID: "11111111-1111-4111-8111-111111111111", ObservedAt: resetAt.Add(-time.Hour), ResetRaw: "resets 4:30pm (UTC)", ResetAt: resetAt, RequestID: requestID}
}
