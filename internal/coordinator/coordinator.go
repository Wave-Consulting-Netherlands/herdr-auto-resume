package coordinator

import (
	"strings"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

type Config struct {
	OwnPaneID   string
	TestPattern string
	DryRun      bool
	ReadLines   int
}

type ActionRecord struct {
	Time   time.Time
	PaneID string
	Kind   string
	DryRun bool
}

// LimitEvent is the complete known-reset evidence observed for one pane poll.
type LimitEvent struct {
	Pane       runtime.Pane
	Provider   string
	ResetsRaw  string
	ResetTime  time.Time
	Spec       detection.ResetSpec
	Content    string
	ObservedAt time.Time
}

// JobSink owns known-reset limit episodes when configured by the headless runner.
type JobSink interface {
	HandleLimit(LimitEvent) bool
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

type Coordinator struct {
	rt         runtime.Runtime
	cfg        Config
	clock      func() time.Time
	sleep      func(time.Duration)
	states     map[string]PaneState
	paneOrder  []string
	lastAction ActionRecord
	hasAction  bool
	jobSink    JobSink
	postPoll   func(now time.Time)
	providers  *provider.Registry
}

func defaultRegistry() *provider.Registry {
	return provider.NewRegistry(claude.New(""))
}

func New(rt runtime.Runtime, cfg Config, opts ...Option) *Coordinator {
	c := &Coordinator{
		rt:        rt,
		cfg:       cfg,
		clock:     time.Now,
		sleep:     time.Sleep,
		states:    make(map[string]PaneState),
		providers: defaultRegistry(),
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
		current := c.providers.Resolve(state.Pane.Agent, content)
		if current == nil {
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
		analysis := current.Analyze(content, now)
		statusLimited := analysis.IsLimited

		wasLimited := state.IsRateLimited
		state.IsRateLimited = statusLimited
		state.RateLimitResets = resetDisplayText(analysis.Reset.Raw)
		state.RateLimitTime = analysis.Reset.ParsedTime

		if !wasLimited && statusLimited {
			state.ContinueSent = false
			state.LastPeriodicContinue = time.Time{}
		}
		if wasLimited && !statusLimited {
			state.ContinueSent = false
			state.LastPeriodicContinue = time.Time{}
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
						ObservedAt: now,
					})
					if owned {
						state.ContinueSent = true
					}
				} else if !analysis.MenuVisible && !state.ContinueSent && now.After(state.RateLimitTime) {
					c.sendResume(paneID, current)
					state.ContinueSent = true
				}
			} else {
				periodicInterval := 15 * time.Minute
				if current.AllowPeriodicNudge() && !analysis.MenuVisible && (state.LastPeriodicContinue.IsZero() || now.Sub(state.LastPeriodicContinue) >= periodicInterval) {
					c.sendResume(paneID, current)
					state.LastPeriodicContinue = now
				}
			}
		}

		if c.cfg.TestPattern != "" &&
			strings.Contains(content, c.cfg.TestPattern) &&
			state.Mode == ModeAuto &&
			!state.ContinueSent {
			c.sendResume(paneID, current)
			state.ContinueSent = true
		}
		c.states[paneID] = *state
	}
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
		c.checkPaneRateLimit(&state)
	} else {
		state.Mode = ModeOff
	}
	c.states[paneID] = state
}

func (c *Coordinator) EnableAll() {
	for _, paneID := range c.paneOrder {
		state := c.states[paneID]
		if stateProviderActive(state) {
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

func (c *Coordinator) sendResume(paneID string, current provider.Provider) {
	if c.cfg.DryRun {
		c.recordAction(paneID, true)
		return
	}

	_ = SendResumeAction(c.rt, paneID, current.ResumeAction(), c.sleep)
	c.recordAction(paneID, false)
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
	var firstErr error
	for _, key := range action.KeysBefore {
		if err := rt.SendKeys(paneID, key); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if len(action.KeysBefore) > 0 {
		sleep(100 * time.Millisecond)
	}
	if err := rt.SendText(paneID, action.Text); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := rt.SendKeys(paneID, action.SubmitKey); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (c *Coordinator) recordAction(paneID string, dryRun bool) {
	c.lastAction = ActionRecord{Time: c.clock(), PaneID: paneID, Kind: "continue", DryRun: dryRun}
	c.hasAction = true
}
