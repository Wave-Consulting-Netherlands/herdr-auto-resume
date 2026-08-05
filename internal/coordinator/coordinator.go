package coordinator

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/sessionfile"
)

type Config struct {
	OwnPaneID           string
	TestPattern         string
	DryRun              bool
	ReadLines           int
	SessionFileChannel  bool
	AdmitSessionMatches bool
	AdmitAgentEvents    bool
	Margin              time.Duration
	VerifyTimeout       time.Duration
}

type ActionRecord struct {
	Time   time.Time
	PaneID string
	Kind   string
	DryRun bool
}

type FailureRecord struct {
	Time   time.Time
	PaneID string
	Err    error
}

// LimitEvent is the complete known-reset evidence observed for one pane poll.
type LimitEvent struct {
	Pane       runtime.Pane
	Provider   string
	Source     string
	EpisodeID  string
	ResetsRaw  string
	ResetTime  time.Time
	Spec       detection.ResetSpec
	Content    string
	Evidence   string
	ObservedAt time.Time
}

// JobSink owns known-reset limit episodes when configured by the headless runner.
type JobSink interface {
	HandleLimit(LimitEvent) bool
}

type SessionFileSource interface {
	Scan() ([]sessionfile.SessionObservation, error)
	Pending() ([]sessionfile.SessionObservation, error)
	CommitPending(requestID, episodeID string) error
}

type sessionFileEpisodeResolver interface {
	ResolveEpisode(sessionfile.SessionObservation) (sessionfile.Episode, bool, error)
}

type Option func(*Coordinator)

func WithClock(clock func() time.Time) Option {
	return func(c *Coordinator) { c.clock = clock }
}

func WithSleep(sleep func(time.Duration)) Option {
	return func(c *Coordinator) { c.sleep = sleep }
}

func WithJobSink(sink JobSink) Option {
	return func(c *Coordinator) { c.jobSink = sink }
}

func WithLogWriter(logw io.Writer) Option {
	return func(c *Coordinator) {
		if logw == nil {
			c.logw = io.Discard
			return
		}
		c.logw = logw
	}
}

func WithPostPoll(postPoll func(now time.Time)) Option {
	return func(c *Coordinator) { c.postPoll = postPoll }
}

func WithProviders(registry *provider.Registry) Option {
	return func(c *Coordinator) {
		if registry == nil {
			c.providers = defaultRegistry()
			return
		}
		c.providers = registry
	}
}

func WithSessionFileSource(source SessionFileSource) Option {
	return func(c *Coordinator) { c.sessionFileSource = source }
}

type Coordinator struct {
	rt                 runtime.Runtime
	cfg                Config
	clock              func() time.Time
	sleep              func(time.Duration)
	states             map[string]PaneState
	paneOrder          []string
	lastAction         ActionRecord
	hasAction          bool
	lastFailure        FailureRecord
	hasFailure         bool
	failedPanes        map[string]bool
	jobSink            JobSink
	postPoll           func(now time.Time)
	providers          *provider.Registry
	logw               io.Writer
	diagnosticEvidence map[string]string
	sessionFileSource  SessionFileSource
}

func defaultRegistry() *provider.Registry {
	return provider.NewRegistry(claude.New(""))
}

func New(rt runtime.Runtime, cfg Config, opts ...Option) *Coordinator {
	c := &Coordinator{
		rt:                 rt,
		cfg:                cfg,
		clock:              time.Now,
		sleep:              time.Sleep,
		states:             make(map[string]PaneState),
		failedPanes:        make(map[string]bool),
		providers:          defaultRegistry(),
		logw:               io.Discard,
		diagnosticEvidence: make(map[string]string),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetPanes refreshes descriptors while preserving state for panes that remain.
// State for panes absent from the refresh is pruned.
func (c *Coordinator) SetPanes(panes []runtime.Pane) {
	updated := make(map[string]PaneState, len(panes))
	order := make([]string, 0, len(panes))
	for _, pane := range panes {
		state, ok := c.states[pane.ID]
		if !ok {
			state = PaneState{}
		}
		state.Pane = pane
		updated[pane.ID] = state
		order = append(order, pane.ID)
	}
	c.states = updated
	c.paneOrder = order
	for paneID := range c.diagnosticEvidence {
		if _, ok := updated[paneID]; !ok {
			delete(c.diagnosticEvidence, paneID)
		}
	}
}

func (c *Coordinator) Poll() {
	for _, paneID := range c.paneOrder {
		stateValue := c.states[paneID]
		state := &stateValue
		if paneID == c.cfg.OwnPaneID {
			state.HasClaudeCode = false
			state.Provider = ""
			c.states[paneID] = *state
			continue
		}

		content, err := c.rt.ReadPane(paneID, c.cfg.ReadLines)
		if err != nil {
			continue
		}

		now := c.clock()
		wasChecked := state.ProviderChecked
		wasActive := stateProviderActive(*state)
		current := c.providers.Resolve(state.Pane.Agent, content)
		if current == nil {
			if analysis := detection.Analyze(content, now); analysis.IsLimited {
				c.logLimitedDiagnostic(state.Pane, "", analysis, content, "provider-unresolved")
			}
			state.ProviderChecked = true
			state.Provider = ""
			state.HasClaudeCode = false
			state.IsRateLimited = false
			state.RateLimitResets = ""
			state.RateLimitTime = time.Time{}
			state.ContinueSent = false
			state.LastPeriodicContinue = time.Time{}
			c.states[paneID] = *state
			continue
		}
		state.Provider = current.Name()
		state.HasClaudeCode = current.Name() == "claude"
		if wasChecked && !wasActive && state.Mode == ModeOff && !state.UserDisabled {
			state.Mode = ModeAuto
		}
		state.ProviderChecked = true
		analysis := current.Analyze(content, now)
		statusLimited := analysis.IsLimited

		wasLimited := state.IsRateLimited
		state.IsRateLimited = statusLimited
		state.RateLimitResets = resetDisplayText(analysis.Reset.Raw)
		state.RateLimitTime = analysis.Reset.ParsedTime

		if !wasLimited && statusLimited {
			state.ContinueSent = false
			state.LastPeriodicContinue = time.Time{}
			delete(c.failedPanes, paneID)
		}
		if wasLimited && !statusLimited {
			state.ContinueSent = false
			state.LastPeriodicContinue = time.Time{}
			delete(c.failedPanes, paneID)
		}

		if state.IsRateLimited {
			switch {
			case state.Mode != ModeAuto && wasChecked:
				c.logLimitedDiagnostic(state.Pane, current.Name(), analysis, content, "not-auto")
			case state.Mode == ModeAuto && analysis.MenuVisible && (c.jobSink == nil || !analysis.Actionable):
				c.logLimitedDiagnostic(state.Pane, current.Name(), analysis, content, "menu-visible")
			case state.Mode == ModeAuto && analysis.Reset.ParsedTime.IsZero():
				c.logLimitedDiagnostic(state.Pane, current.Name(), analysis, content, "reset-unparsed")
			case state.Mode == ModeAuto && !analysis.Actionable:
				c.logLimitedDiagnostic(state.Pane, current.Name(), analysis, content, "not-actionable")
			}
		}

		if state.IsRateLimited && state.Mode == ModeAuto && analysis.Actionable {
			if !analysis.Reset.ParsedTime.IsZero() {
				if c.jobSink != nil {
					owned := c.jobSink.HandleLimit(LimitEvent{
						Pane:       state.Pane,
						Provider:   current.Name(),
						ResetsRaw:  resetDisplayText(analysis.Reset.Raw),
						ResetTime:  analysis.Reset.ParsedTime,
						Spec:       analysis.Reset,
						Content:    content,
						Evidence:   analysis.Evidence,
						ObservedAt: now,
					})
					if owned {
						state.ContinueSent = true
					} else {
						c.logLimitedDiagnostic(state.Pane, current.Name(), analysis, content, "job-manager-declined")
					}
				} else {
					c.logLimitedDiagnostic(state.Pane, current.Name(), analysis, content, "job-manager-unavailable")
					if !analysis.MenuVisible && !state.ContinueSent && !c.failedPanes[paneID] && now.After(state.RateLimitTime) {
						if c.sendResume(paneID, current) == nil {
							state.ContinueSent = true
						}
					}
				}
			} else {
				periodicInterval := 15 * time.Minute
				if current.AllowPeriodicNudge() && !analysis.MenuVisible && !c.failedPanes[paneID] && (state.LastPeriodicContinue.IsZero() || now.Sub(state.LastPeriodicContinue) >= periodicInterval) {
					if c.sendResume(paneID, current) == nil {
						state.LastPeriodicContinue = now
					}
				}
			}
		}

		if c.cfg.TestPattern != "" &&
			strings.Contains(content, c.cfg.TestPattern) &&
			state.Mode == ModeAuto &&
			!state.ContinueSent && !c.failedPanes[paneID] {
			if c.sendResume(paneID, current) == nil {
				state.ContinueSent = true
			}
		}
		c.states[paneID] = *state
	}
}

// AdmitSessionFilePanes examines a complete, unfiltered pane snapshot before
// the normal monitored-pane filter runs. Admission is deliberately kept in
// the coordinator loop: the caller mutates its in-memory monitored set in
// admit, while the observation remains pending until ProcessSessionFile
// creates or deduplicates the job. The set is intentionally not persisted;
// an admitted pane must be re-admitted after watcher restart by a fresh
// observation.
func (c *Coordinator) AdmitSessionFilePanes(panes []runtime.Pane, complete bool, isMonitored func(runtime.Pane) bool, selfPaneID string, admit func(runtime.Pane), now time.Time) {
	if !c.cfg.SessionFileChannel || !c.cfg.AdmitSessionMatches || c.sessionFileSource == nil || !complete || admit == nil {
		return
	}
	if _, err := c.sessionFileSource.Scan(); err != nil {
		c.logf("session-file admission scan failed: %v", err)
		return
	}
	pending, err := c.sessionFileSource.Pending()
	if err != nil {
		c.logf("session-file admission pending load failed: %v", err)
		return
	}
	verifyTimeout := c.cfg.VerifyTimeout
	if verifyTimeout <= 0 {
		verifyTimeout = 90 * time.Second
	}
	margin := c.cfg.Margin
	if margin < 0 {
		margin = 0
	}
	for _, observation := range pending {
		if sessionFileObservationExpired(observation, now, margin, verifyTimeout) {
			continue
		}
		matches := make([]runtime.Pane, 0, 1)
		for _, pane := range panes {
			if pane.AgentSessionID == observation.SessionID {
				matches = append(matches, pane)
			}
		}
		if len(matches) != 1 {
			c.logf("session-file admission request=%s pending: %d agent_session matches", observation.RequestID, len(matches))
			continue
		}
		pane := matches[0]
		if selfPaneID != "" && pane.ID == selfPaneID {
			c.logf("session-file admission request=%s refused: self pane=%s", observation.RequestID, pane.ID)
			continue
		}
		if !strings.EqualFold(pane.Agent, observation.Provider) {
			c.logf("session-file admission request=%s refused: provider mismatch pane=%s pane_provider=%q observation_provider=%q", observation.RequestID, pane.ID, pane.Agent, observation.Provider)
			continue
		}
		if pane.CWD != observation.CWD {
			c.logf("session-file admission request=%s refused: cwd mismatch pane=%s pane_cwd=%q observation_cwd=%q", observation.RequestID, pane.ID, pane.CWD, observation.CWD)
			continue
		}
		if isMonitored != nil && isMonitored(pane) {
			continue
		}
		episodeID := fmt.Sprintf("%s/%s/%s", observation.Provider, observation.SessionID, observation.ResetAt.UTC().Format(time.RFC3339))
		if resolver, ok := c.sessionFileSource.(sessionFileEpisodeResolver); ok {
			episode, _, err := resolver.ResolveEpisode(observation)
			if err != nil {
				c.logf("session-file admission request=%s episode resolve failed: %v", observation.RequestID, err)
				continue
			}
			episodeID = episode.ID
		}
		c.logf("session-file admission: admitted pane=%s session=%s episode=%s", pane.ID, observation.SessionID, episodeID)
		admit(pane)
	}
}

// AdmitAgentEventPanes reconciles an agent-detected event against a fresh,
// complete pane snapshot. The event is only a trigger: the current snapshot is
// authoritative, so replayed lifecycle frames for vanished panes cannot admit
// anything. Admission changes coverage only; Poll and the job manager retain
// all provider, identity, process, cwd, menu, and safety gates.
func (c *Coordinator) AdmitAgentEventPanes(panes []runtime.Pane, eventPanes map[string]string, complete bool, isMonitored func(runtime.Pane) bool, selfPaneID string, admit func(runtime.Pane), now time.Time) {
	if len(eventPanes) == 0 {
		return
	}
	c.admitAgentPanes(panes, eventPanes, complete, isMonitored, selfPaneID, admit, "pane.agent_detected")
}

// AdmitAgentSnapshotPanes seeds agent-event admission from a complete pane
// snapshot. The same narrow admission checks as the event path apply; this
// only fills the coverage gap for panes detected before subscription or while
// the event stream was unavailable.
func (c *Coordinator) AdmitAgentSnapshotPanes(panes []runtime.Pane, complete bool, isMonitored func(runtime.Pane) bool, selfPaneID string, admit func(runtime.Pane), now time.Time) {
	c.admitAgentPanes(panes, nil, complete, isMonitored, selfPaneID, admit, "startup-snapshot")
}

func (c *Coordinator) admitAgentPanes(panes []runtime.Pane, eventPanes map[string]string, complete bool, isMonitored func(runtime.Pane) bool, selfPaneID string, admit func(runtime.Pane), trigger string) {
	if !c.cfg.AdmitAgentEvents || !complete || admit == nil {
		return
	}
	for _, pane := range panes {
		reportedAgent := pane.Agent
		if eventPanes != nil {
			var triggered bool
			reportedAgent, triggered = eventPanes[pane.ID]
			if !triggered {
				continue
			}
		}
		if pane.ID == "" || (selfPaneID != "" && pane.ID == selfPaneID) {
			continue
		}
		if state, ok := c.states[pane.ID]; ok && state.UserDisabled {
			continue
		}
		if isMonitored != nil && isMonitored(pane) {
			continue
		}
		reportedProvider := c.providers.Resolve(reportedAgent, "")
		currentProvider := c.providers.Resolve(pane.Agent, "")
		if reportedProvider == nil || currentProvider == nil || !strings.EqualFold(reportedProvider.Name(), currentProvider.Name()) {
			continue
		}
		c.logf("agent-event admission: admitted pane=%s agent=%s trigger=%s", pane.ID, pane.Agent, trigger)
		admit(pane)
	}
}

// ProcessSessionFile scans and resolves pending file observations against the
// fresh monitored-pane snapshot. It runs in the coordinator's serialized loop.
func (c *Coordinator) ProcessSessionFile(panes []runtime.Pane, now time.Time) {
	if !c.cfg.SessionFileChannel || c.sessionFileSource == nil {
		return
	}
	if _, err := c.sessionFileSource.Scan(); err != nil {
		c.logf("session-file scan failed: %v", err)
		return
	}
	pending, err := c.sessionFileSource.Pending()
	if err != nil {
		c.logf("session-file pending load failed: %v", err)
		return
	}
	verifyTimeout := c.cfg.VerifyTimeout
	if verifyTimeout <= 0 {
		verifyTimeout = 90 * time.Second
	}
	margin := c.cfg.Margin
	if margin < 0 {
		margin = 0
	}
	for _, observation := range pending {
		observedAt := observation.ObservedAt
		if observedAt.IsZero() {
			observedAt = now
		}
		if sessionFileObservationExpired(observation, now, margin, verifyTimeout) {
			_ = c.sessionFileSource.CommitPending(observation.RequestID, "rejected")
			continue
		}
		matches := make([]runtime.Pane, 0, 1)
		for _, pane := range panes {
			if pane.AgentSessionID != observation.SessionID || !strings.EqualFold(pane.Agent, observation.Provider) || pane.CWD != observation.CWD {
				continue
			}
			matches = append(matches, pane)
		}
		if len(matches) != 1 {
			c.logf("session-file request=%s pending: %d consistent monitored pane matches", observation.RequestID, len(matches))
			continue
		}
		if c.jobSink == nil {
			c.logf("session-file request=%s pending: job manager unavailable", observation.RequestID)
			continue
		}
		spec := detection.ParseReset(observation.ResetRaw, observedAt)
		event := LimitEvent{
			Pane: matches[0], Provider: observation.Provider, Source: "session-file",
			ResetsRaw: observation.ResetRaw, ResetTime: observation.ResetAt, Spec: spec,
			Content: observation.ResetRaw, Evidence: observation.ResetRaw, ObservedAt: observedAt,
		}
		if resolver, ok := c.sessionFileSource.(sessionFileEpisodeResolver); ok {
			episode, _, err := resolver.ResolveEpisode(observation)
			if err != nil {
				c.logf("session-file request=%s episode resolve failed: %v", observation.RequestID, err)
				continue
			}
			event.EpisodeID = episode.ID
		}
		if c.jobSink.HandleLimit(event) {
			if err := c.sessionFileSource.CommitPending(observation.RequestID, event.EpisodeID); err != nil {
				c.logf("session-file request=%s commit failed: %v", observation.RequestID, err)
			}
		}
	}
}

func sessionFileObservationExpired(observation sessionfile.SessionObservation, now time.Time, margin, verifyTimeout time.Duration) bool {
	observedAt := observation.ObservedAt
	if observedAt.IsZero() {
		observedAt = now
	}
	expiresAt := observation.ResetAt.Add(verifyTimeout)
	if alt := observedAt.Add(24 * time.Hour); alt.Before(expiresAt) {
		expiresAt = alt
	}
	return !observation.ResetAt.IsZero() && (now.After(observation.ResetAt.Add(margin)) || !now.Before(expiresAt))
}

func (c *Coordinator) logf(format string, args ...any) {
	fmt.Fprintf(c.logw, format+"\n", args...)
}

func (c *Coordinator) Snapshot() []PaneState {
	snapshot := make([]PaneState, 0, len(c.paneOrder))
	for _, paneID := range c.paneOrder {
		snapshot = append(snapshot, c.states[paneID])
	}
	return snapshot
}

func (c *Coordinator) ToggleMode(paneID string) {
	state, ok := c.states[paneID]
	if !ok || !stateProviderActive(state) {
		return
	}
	if state.Mode == ModeOff {
		state.Mode = ModeAuto
		state.UserDisabled = false
		c.checkPaneRateLimit(&state)
	} else {
		state.Mode = ModeOff
		state.UserDisabled = true
	}
	c.states[paneID] = state
}

func (c *Coordinator) EnableAll() {
	for _, paneID := range c.paneOrder {
		state := c.states[paneID]
		if stateProviderActive(state) && !state.UserDisabled {
			state.Mode = ModeAuto
			c.checkPaneRateLimit(&state)
			c.states[paneID] = state
		}
	}
}

func (c *Coordinator) DisableAll() {
	for _, paneID := range c.paneOrder {
		state := c.states[paneID]
		if stateProviderActive(state) {
			state.Mode = ModeOff
			state.UserDisabled = true
			c.states[paneID] = state
		}
	}
}

func stateProviderActive(state PaneState) bool {
	return state.Provider != "" || state.HasClaudeCode
}

func (c *Coordinator) LastAction() (ActionRecord, bool) {
	return c.lastAction, c.hasAction
}

func (c *Coordinator) LastFailure() (FailureRecord, bool) {
	return c.lastFailure, c.hasFailure
}

func (c *Coordinator) checkPaneRateLimit(state *PaneState) {
	if state == nil || state.Pane.ID == c.cfg.OwnPaneID {
		return
	}
	content, err := c.rt.ReadPane(state.Pane.ID, c.cfg.ReadLines)
	if err != nil {
		return
	}
	current := c.providers.Resolve(state.Pane.Agent, content)
	if current == nil {
		if analysis := detection.Analyze(content, c.clock()); analysis.IsLimited {
			c.logLimitedDiagnostic(state.Pane, "", analysis, content, "provider-unresolved")
		}
		state.Provider = ""
		state.HasClaudeCode = false
		state.IsRateLimited = false
		state.RateLimitResets = ""
		state.RateLimitTime = time.Time{}
		state.ContinueSent = false
		return
	}
	state.Provider = current.Name()
	state.HasClaudeCode = current.Name() == "claude"
	analysis := current.Analyze(content, c.clock())
	state.IsRateLimited = analysis.IsLimited
	state.RateLimitResets = resetDisplayText(analysis.Reset.Raw)
	state.RateLimitTime = analysis.Reset.ParsedTime
	state.ContinueSent = false
}

func (c *Coordinator) sendResume(paneID string, current provider.Provider) error {
	if c.cfg.DryRun {
		c.recordAction(paneID, true)
		return nil
	}

	if err := SendResumeAction(c.rt, paneID, current.ResumeAction(), c.sleep); err != nil {
		c.failedPanes[paneID] = true
		c.lastFailure = FailureRecord{Time: c.clock(), PaneID: paneID, Err: err}
		c.hasFailure = true
		return err
	}
	c.recordAction(paneID, false)
	return nil
}

func resetDisplayText(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.LastIndex(raw, "("); i >= 0 && strings.HasSuffix(raw, ")") {
		return strings.TrimSpace(raw[:i])
	}
	return raw
}

// SendContinueSequence submits the one allow-listed continuation action.
func SendContinueSequence(rt runtime.Runtime, paneID string, sleep func(time.Duration)) error {
	return SendResumeAction(rt, paneID, claude.New("").ResumeAction(), sleep)
}

// SendResumeAction executes a provider's structured action. The sleep remains
// between Claude's Escape and text exactly as it was before provider support.
func SendResumeAction(rt runtime.Runtime, paneID string, action provider.ResumeAction, sleep func(time.Duration)) error {
	if sleep == nil {
		sleep = time.Sleep
	}
	for _, key := range action.KeysBefore {
		if err := rt.SendKeys(paneID, key); err != nil {
			return err
		}
	}
	if len(action.KeysBefore) > 0 {
		sleep(100 * time.Millisecond)
	}
	if err := rt.SendText(paneID, action.Text); err != nil {
		return err
	}
	return rt.SendKeys(paneID, action.SubmitKey)
}

func (c *Coordinator) recordAction(paneID string, dryRun bool) {
	c.lastAction = ActionRecord{Time: c.clock(), PaneID: paneID, Kind: "continue", DryRun: dryRun}
	c.hasAction = true
}
