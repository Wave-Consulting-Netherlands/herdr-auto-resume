package sessionfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const episodeTolerance = 10 * time.Minute

// Episode is the durable identity shared by session-file and scrape events.
// FirstResetAt is retained rather than replaced when a later observer reports
// a reset within the tolerance window.
type Episode struct {
	ID           string
	Provider     string
	SessionID    string
	FirstResetAt time.Time
}

type episodeRecord struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	SessionID    string    `json:"session_id"`
	FirstResetAt time.Time `json:"first_reset_at"`
}

// EpisodeRegistry stores episode identity in the scanner sidecar. It uses the
// same exclusive scan lock and atomic sidecar writer as Scanner.
type EpisodeRegistry struct {
	statePath string
}

// ResolveEpisode lets the coordinator use the same sidecar registry before
// handing an event to the job sink. The bool reports whether it already
// existed, matching Resolve's duplicate result.
func (r *EpisodeRegistry) ResolveEpisode(observation SessionObservation) (Episode, bool, error) {
	return r.Resolve(observation)
}

// NewEpisodeRegistry creates a registry backed by <state>.scan.json.
func NewEpisodeRegistry(statePath string) (*EpisodeRegistry, error) {
	if statePath == "" {
		return nil, errors.New("episode registry state path is required")
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		return nil, fmt.Errorf("create episode registry state directory: %w", err)
	}
	return &EpisodeRegistry{statePath: statePath}, nil
}

// Resolve returns the nearest first-seen episode within the inclusive
// tolerance. duplicate is true when no new registry entry was created.
func (r *EpisodeRegistry) Resolve(observation SessionObservation) (Episode, bool, error) {
	if observation.Provider == "" || observation.SessionID == "" || observation.ResetAt.IsZero() {
		return Episode{}, false, errors.New("episode requires provider, session, and reset time")
	}
	accessor := &Scanner{statePath: r.statePath}
	lock, err := accessor.lock()
	if err != nil {
		return Episode{}, false, err
	}
	defer unlock(lock)
	state, err := accessor.readSidecar()
	if err != nil {
		return Episode{}, false, err
	}
	if episode, ok := nearestEpisode(state.Episodes, observation); ok {
		return episode, true, nil
	}
	episode := Episode{
		ID:           episodeID(observation.Provider, observation.SessionID, observation.ResetAt),
		Provider:     observation.Provider,
		SessionID:    observation.SessionID,
		FirstResetAt: observation.ResetAt.UTC(),
	}
	state.Episodes = append(state.Episodes, episodeRecord{ID: episode.ID, Provider: episode.Provider, SessionID: episode.SessionID, FirstResetAt: episode.FirstResetAt})
	if err := accessor.writeSidecar(state); err != nil {
		return Episode{}, false, err
	}
	return episode, false, nil
}

// Lookup returns an existing episode without creating one.
func (r *EpisodeRegistry) Lookup(observation SessionObservation) (Episode, bool, error) {
	accessor := &Scanner{statePath: r.statePath}
	lock, err := accessor.lock()
	if err != nil {
		return Episode{}, false, err
	}
	defer unlock(lock)
	state, err := accessor.readSidecar()
	if err != nil {
		return Episode{}, false, err
	}
	episode, ok := nearestEpisode(state.Episodes, observation)
	return episode, ok, nil
}

func nearestEpisode(records []episodeRecord, observation SessionObservation) (Episode, bool) {
	best := episodeRecord{}
	bestDelta := episodeTolerance + time.Second
	for _, record := range records {
		if record.Provider != observation.Provider || record.SessionID != observation.SessionID {
			continue
		}
		delta := record.FirstResetAt.Sub(observation.ResetAt)
		if delta < 0 {
			delta = -delta
		}
		if delta <= episodeTolerance && delta < bestDelta {
			best, bestDelta = record, delta
		}
	}
	if best.ID == "" {
		return Episode{}, false
	}
	return Episode{ID: best.ID, Provider: best.Provider, SessionID: best.SessionID, FirstResetAt: best.FirstResetAt}, true
}

func episodeID(provider, sessionID string, resetAt time.Time) string {
	return fmt.Sprintf("%s:%s:%d", provider, sessionID, resetAt.UTC().UnixNano())
}
