package coordinator

import (
	"testing"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/sessionfile"
)

type fakeSessionFileSource struct {
	observations []sessionfile.SessionObservation
	pending      []sessionfile.SessionObservation
	committed    []string
}

func (s *fakeSessionFileSource) Scan() ([]sessionfile.SessionObservation, error) {
	got := append([]sessionfile.SessionObservation(nil), s.observations...)
	s.observations = nil
	return got, nil
}

func (s *fakeSessionFileSource) Pending() ([]sessionfile.SessionObservation, error) {
	return append([]sessionfile.SessionObservation(nil), s.pending...), nil
}

func (s *fakeSessionFileSource) CommitPending(requestID, _ string) error {
	s.committed = append(s.committed, requestID)
	remaining := s.pending[:0]
	for _, observation := range s.pending {
		if observation.RequestID != requestID {
			remaining = append(remaining, observation)
		}
	}
	s.pending = remaining
	return nil
}

func TestSessionFileMonitoredMatchCreatesKnownResetJobEvent(t *testing.T) {
	now := time.Date(2026, 8, 1, 16, 0, 0, 0, time.UTC)
	observation := sessionObservation("request", now.Add(30*time.Minute))
	source := &fakeSessionFileSource{observations: []sessionfile.SessionObservation{observation}, pending: []sessionfile.SessionObservation{observation}}
	sink := &recordingJobSink{owned: true}
	pane := runtime.Pane{ID: "p1", Agent: "claude", AgentSessionID: observation.SessionID, CWD: observation.CWD}
	c := New(&runtime.Fake{}, Config{SessionFileChannel: true, Margin: time.Minute, VerifyTimeout: time.Hour}, WithClock(func() time.Time { return now }), WithJobSink(sink), WithSessionFileSource(source))
	c.ProcessSessionFile([]runtime.Pane{pane}, now)
	if len(sink.events) != 1 || sink.events[0].Source != "session-file" || sink.events[0].Pane != pane || sink.events[0].Provider != "claude" {
		t.Fatalf("events = %#v, want one session-file event for exact pane", sink.events)
	}
	if len(source.pending) != 0 || len(source.committed) != 1 {
		t.Fatalf("pending=%#v committed=%#v, want committed observation", source.pending, source.committed)
	}
}

func TestSessionFileLaggingAgentSessionRetriesUntilFreshSnapshotMatches(t *testing.T) {
	now := time.Date(2026, 8, 1, 16, 0, 0, 0, time.UTC)
	observation := sessionObservation("lagging", now.Add(30*time.Minute))
	source := &fakeSessionFileSource{observations: []sessionfile.SessionObservation{observation}, pending: []sessionfile.SessionObservation{observation}}
	sink := &recordingJobSink{owned: true}
	pane := runtime.Pane{ID: "p1", Agent: "claude", CWD: observation.CWD}
	c := New(&runtime.Fake{}, Config{SessionFileChannel: true, Margin: time.Minute, VerifyTimeout: time.Hour}, WithClock(func() time.Time { return now }), WithJobSink(sink), WithSessionFileSource(source))
	c.ProcessSessionFile([]runtime.Pane{pane}, now)
	if len(sink.events) != 0 || len(source.pending) != 1 {
		t.Fatalf("lagging pass events=%d pending=%d, want pending", len(sink.events), len(source.pending))
	}
	pane.AgentSessionID = observation.SessionID
	c.ProcessSessionFile([]runtime.Pane{pane}, now.Add(time.Minute))
	if len(sink.events) != 1 || len(source.pending) != 0 {
		t.Fatalf("matching retry events=%d pending=%d, want one committed event", len(sink.events), len(source.pending))
	}
}

func TestSessionFileExpiryAndStaleResetAreRejected(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	stale := sessionObservation("stale", now.Add(-2*time.Minute))
	expired := sessionObservation("expired", now.Add(-time.Hour))
	source := &fakeSessionFileSource{pending: []sessionfile.SessionObservation{stale, expired}}
	sink := &recordingJobSink{owned: true}
	c := New(&runtime.Fake{}, Config{SessionFileChannel: true, Margin: time.Minute, VerifyTimeout: 10 * time.Minute}, WithClock(func() time.Time { return now }), WithJobSink(sink), WithSessionFileSource(source))
	c.ProcessSessionFile(nil, now)
	if len(sink.events) != 0 || len(source.pending) != 0 || len(source.committed) != 2 {
		t.Fatalf("events=%d pending=%d committed=%d, want both rejected", len(sink.events), len(source.pending), len(source.committed))
	}
}

func TestSessionFileChannelOffDoesNothing(t *testing.T) {
	source := &fakeSessionFileSource{pending: []sessionfile.SessionObservation{sessionObservation("off", time.Now().Add(time.Hour))}}
	sink := &recordingJobSink{owned: true}
	c := New(&runtime.Fake{}, Config{}, WithJobSink(sink), WithSessionFileSource(source))
	c.ProcessSessionFile(nil, time.Now())
	if len(sink.events) != 0 || len(source.pending) != 1 {
		t.Fatalf("events=%d pending=%d, channel off must preserve no behavior", len(sink.events), len(source.pending))
	}
}

func TestScrapeLimitEventRetainsAgentSessionIdentity(t *testing.T) {
	content := "limit reached ∙ resets 2pm"
	now := detection.CheckRateLimitAt(content, coordinatorTestNow).ResetTime.Add(5 * time.Minute)
	pane := runtime.Pane{ID: "p1", Agent: "claude", AgentSessionID: "11111111-1111-4111-8111-111111111111"}
	fake := &runtime.Fake{PanesList: []runtime.Pane{pane}, Content: map[string]string{"p1": content}}
	sink := &recordingJobSink{owned: true}
	c := New(fake, Config{ReadLines: 10}, WithClock(func() time.Time { return now }), WithJobSink(sink))
	c.SetPanes(fake.PanesList)
	c.Poll()
	c.ToggleMode("p1")
	c.Poll()
	if len(sink.events) != 1 || sink.events[0].Pane.AgentSessionID != pane.AgentSessionID {
		t.Fatalf("scrape event = %#v, want pane session identity retained", sink.events)
	}
}

func TestSessionFileRejectsZeroOrMultipleConsistencyMatches(t *testing.T) {
	now := time.Date(2026, 8, 1, 16, 0, 0, 0, time.UTC)
	observation := sessionObservation("multiple", now.Add(time.Hour))
	source := &fakeSessionFileSource{pending: []sessionfile.SessionObservation{observation}}
	sink := &recordingJobSink{owned: true}
	matching := runtime.Pane{ID: "p1", Agent: "claude", AgentSessionID: observation.SessionID, CWD: observation.CWD}
	other := matching
	other.ID = "p2"
	c := New(&runtime.Fake{}, Config{SessionFileChannel: true, Margin: time.Minute, VerifyTimeout: time.Hour}, WithJobSink(sink), WithSessionFileSource(source))
	c.ProcessSessionFile([]runtime.Pane{matching, other}, now)
	if len(sink.events) != 0 || len(source.pending) != 1 {
		t.Fatal("multiple matches must remain pending")
	}
}

func sessionObservation(requestID string, resetAt time.Time) sessionfile.SessionObservation {
	return sessionfile.SessionObservation{Provider: "claude", SessionID: "11111111-1111-4111-8111-111111111111", CWD: "/work", ObservedAt: resetAt.Add(-time.Hour), ResetRaw: "You've hit your session limit · resets 4:30pm (UTC)", ResetAt: resetAt, RequestID: requestID}
}
