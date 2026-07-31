package coordinator

import (
	"strings"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
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
	ResetsRaw  string
	ResetTime  time.Time
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
}

func New(rt runtime.Runtime, cfg Config, opts ...Option) *Coordinator {
	c := &Coordinator{
		rt:     rt,
		cfg:    cfg,
		clock:  time.Now,
		sleep:  time.Sleep,
		states: make(map[string]PaneState),
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
			c.states[paneID] = *state
			continue
		}

		content, err := c.rt.ReadPane(paneID, c.cfg.ReadLines)
		if err != nil {
			continue
		}

		state.HasClaudeCode = detection.IsClaudeCode(content)
		if state.HasClaudeCode {
			status := detection.CheckRateLimit(content)

			wasLimited := state.IsRateLimited
			state.IsRateLimited = status.IsLimited
			state.RateLimitResets = status.ResetsAt
			state.RateLimitTime = status.ResetTime

			if !wasLimited && status.IsLimited {
				state.ContinueSent = false
				state.LastPeriodicContinue = time.Time{}
			}

			if state.IsRateLimited && state.Mode == ModeAuto {
				now := c.clock()
				if !state.RateLimitTime.IsZero() {
					if c.jobSink != nil {
						owned := c.jobSink.HandleLimit(LimitEvent{
							Pane:       state.Pane,
							ResetsRaw:  status.ResetsAt,
							ResetTime:  status.ResetTime,
							Content:    content,
							ObservedAt: now,
						})
						if owned {
							state.ContinueSent = true
						}
					} else if !state.ContinueSent && now.After(state.RateLimitTime) {
						c.sendContinue(paneID)
						state.ContinueSent = true
					}
				} else {
					periodicInterval := 15 * time.Minute
					if state.LastPeriodicContinue.IsZero() || now.Sub(state.LastPeriodicContinue) >= periodicInterval {
						c.sendContinue(paneID)
						state.LastPeriodicContinue = now
					}
				}
			}

			if c.cfg.TestPattern != "" &&
				strings.Contains(content, c.cfg.TestPattern) &&
				state.Mode == ModeAuto &&
				!state.ContinueSent {
				c.sendContinue(paneID)
				state.ContinueSent = true
			}
		} else {
			state.IsRateLimited = false
			state.RateLimitResets = ""
			state.RateLimitTime = time.Time{}
			state.ContinueSent = false
			state.LastPeriodicContinue = time.Time{}
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
	if !ok || !state.HasClaudeCode {
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
		if state.HasClaudeCode {
			state.Mode = ModeAuto
			c.checkPaneRateLimit(&state)
			c.states[paneID] = state
		}
	}
}

func (c *Coordinator) DisableAll() {
	for _, paneID := range c.paneOrder {
		state := c.states[paneID]
		if state.HasClaudeCode {
			state.Mode = ModeOff
			c.states[paneID] = state
		}
	}
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
	status := detection.CheckRateLimit(content)
	state.IsRateLimited = status.IsLimited
	state.RateLimitResets = status.ResetsAt
	state.RateLimitTime = status.ResetTime
	state.ContinueSent = false
}

func (c *Coordinator) sendContinue(paneID string) {
	if c.cfg.DryRun {
		c.recordAction(paneID, true)
		return
	}

	_ = SendContinueSequence(c.rt, paneID, c.sleep)
	c.recordAction(paneID, false)
}

// SendContinueSequence submits the one allow-listed continuation action.
func SendContinueSequence(rt runtime.Runtime, paneID string, sleep func(time.Duration)) error {
	if sleep == nil {
		sleep = time.Sleep
	}
	var firstErr error
	if err := rt.SendKeys(paneID, runtime.KeyEscape); err != nil && firstErr == nil {
		firstErr = err
	}
	sleep(100 * time.Millisecond)
	if err := rt.SendText(paneID, "continue"); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := rt.SendKeys(paneID, runtime.KeyEnter); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (c *Coordinator) recordAction(paneID string, dryRun bool) {
	c.lastAction = ActionRecord{Time: c.clock(), PaneID: paneID, Kind: "continue", DryRun: dryRun}
	c.hasAction = true
}
