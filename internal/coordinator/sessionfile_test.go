package coordinator

import (
	"bytes"
	"strings"
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

func TestSessionFileAdmissionHappyPathCreatesEvent(t *testing.T) {
	now := time.Date(2026, 8, 1, 16, 0, 0, 0, time.UTC)
	observation := sessionObservation("admit", now.Add(time.Hour))
	source := &fakeSessionFileSource{observations: []sessionfile.SessionObservation{observation}, pending: []sessionfile.SessionObservation{observation}}
	sink := &recordingJobSink{owned: true}
	pane := runtime.Pane{ID: "outside", TerminalID: "term-outside", Agent: "claude", AgentSessionID: observation.SessionID, CWD: observation.CWD}
	var admitted runtime.Pane
	var log bytes.Buffer
	c := New(&runtime.Fake{}, Config{SessionFileChannel: true, AdmitSessionMatches: true, Margin: time.Minute, VerifyTimeout: time.Hour}, WithJobSink(sink), WithSessionFileSource(source), WithLogWriter(&log))
	c.AdmitSessionFilePanes([]runtime.Pane{pane}, true, func(runtime.Pane) bool { return false }, "self", func(got runtime.Pane) { admitted = got }, now)
	c.ProcessSessionFile([]runtime.Pane{admitted}, now)
	if admitted != pane || len(sink.events) != 1 || sink.events[0].Source != "session-file" || len(source.pending) != 0 {
		t.Fatalf("admitted=%#v events=%#v pending=%#v, want admitted pane and committed event", admitted, sink.events, source.pending)
	}
	if !strings.Contains(log.String(), "admitted") || !strings.Contains(log.String(), pane.ID) || !strings.Contains(log.String(), observation.SessionID) {
		t.Fatalf("admission log=%q, want pane/session diagnostic", log.String())
	}
}

func TestSessionFileAdmissionRefusesCWDAndMultipleMatches(t *testing.T) {
	now := time.Date(2026, 8, 1, 16, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		panes []runtime.Pane
		want  string
	}{
		{name: "cwd mismatch", panes: []runtime.Pane{{ID: "p1", Agent: "claude", AgentSessionID: "id", CWD: "/wrong"}}, want: "cwd"},
		{name: "multiple", panes: []runtime.Pane{{ID: "p1", Agent: "claude", AgentSessionID: "id", CWD: "/work"}, {ID: "p2", Agent: "claude", AgentSessionID: "id", CWD: "/work"}}, want: "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observation := sessionObservation(tc.name, now.Add(time.Hour))
			observation.SessionID = "id"
			source := &fakeSessionFileSource{pending: []sessionfile.SessionObservation{observation}}
			var admitted runtime.Pane
			var log bytes.Buffer
			c := New(&runtime.Fake{}, Config{SessionFileChannel: true, AdmitSessionMatches: true, Margin: time.Minute, VerifyTimeout: time.Hour}, WithSessionFileSource(source), WithLogWriter(&log))
			c.AdmitSessionFilePanes(tc.panes, true, func(runtime.Pane) bool { return false }, "self", func(pane runtime.Pane) { admitted = pane }, now)
			if admitted.ID != "" || len(source.pending) != 1 || !strings.Contains(log.String(), tc.want) {
				t.Fatalf("admitted=%#v pending=%d log=%q, want refusal diagnostic %q", admitted, len(source.pending), log.String(), tc.want)
			}
		})
	}
}

func TestSessionFileAdmissionFailsClosedForSelfIncompleteAndChannelOff(t *testing.T) {
	now := time.Date(2026, 8, 1, 16, 0, 0, 0, time.UTC)
	observation := sessionObservation("fail-closed", now.Add(time.Hour))
	for _, tc := range []struct {
		name     string
		cfg      Config
		complete bool
		paneID   string
	}{
		{name: "self", cfg: Config{SessionFileChannel: true, AdmitSessionMatches: true}, complete: true, paneID: "self"},
		{name: "incomplete", cfg: Config{SessionFileChannel: true, AdmitSessionMatches: true}, complete: false, paneID: "outside"},
		{name: "channel off", cfg: Config{AdmitSessionMatches: true}, complete: true, paneID: "outside"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pane := runtime.Pane{ID: tc.paneID, Agent: "claude", AgentSessionID: observation.SessionID, CWD: observation.CWD}
			source := &fakeSessionFileSource{pending: []sessionfile.SessionObservation{observation}}
			var admitted bool
			c := New(&runtime.Fake{}, tc.cfg, WithSessionFileSource(source))
			c.AdmitSessionFilePanes([]runtime.Pane{pane}, tc.complete, func(runtime.Pane) bool { return false }, "self", func(runtime.Pane) { admitted = true }, now)
			if admitted || len(source.pending) != 1 {
				t.Fatalf("admitted=%v pending=%d, want no admission", admitted, len(source.pending))
			}
		})
	}
}

func TestSessionFileAdmissionReevaluatesProviderIdentity(t *testing.T) {
	now := time.Date(2026, 8, 1, 16, 0, 0, 0, time.UTC)
	observation := sessionObservation("provider-lag", now.Add(time.Hour))
	source := &fakeSessionFileSource{pending: []sessionfile.SessionObservation{observation}}
	var admitted runtime.Pane
	c := New(&runtime.Fake{}, Config{SessionFileChannel: true, AdmitSessionMatches: true}, WithSessionFileSource(source))
	pane := runtime.Pane{ID: "outside", AgentSessionID: observation.SessionID, CWD: observation.CWD}
	c.AdmitSessionFilePanes([]runtime.Pane{pane}, true, func(runtime.Pane) bool { return false }, "self", func(p runtime.Pane) { admitted = p }, now)
	if admitted.ID != "" {
		t.Fatal("unidentifiable provider was admitted")
	}
	pane.Agent = observation.Provider
	c.AdmitSessionFilePanes([]runtime.Pane{pane}, true, func(runtime.Pane) bool { return false }, "self", func(p runtime.Pane) { admitted = p }, now.Add(time.Minute))
	if admitted.ID != pane.ID {
		t.Fatalf("admitted=%#v after provider became identifiable, want %q", admitted, pane.ID)
	}
}

func sessionObservation(requestID string, resetAt time.Time) sessionfile.SessionObservation {
	return sessionfile.SessionObservation{Provider: "claude", SessionID: "11111111-1111-4111-8111-111111111111", CWD: "/work", ObservedAt: resetAt.Add(-time.Hour), ResetRaw: "You've hit your session limit · resets 4:30pm (UTC)", ResetAt: resetAt, RequestID: requestID}
}
