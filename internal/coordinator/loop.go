package coordinator

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

// RunLoop refreshes and polls coordinator state once for each supplied tick.
// It returns when the context is cancelled or the tick channel is closed.
func (c *Coordinator) RunLoop(ctx context.Context, ticks <-chan time.Time, refresh func() ([]runtime.Pane, error), logw io.Writer) {
	c.RunLoopWithCadence(ctx, ticks, nil, refresh, c.postPoll, logw)
}

// RunLoopWithCadence polls on detectionTicks and uses statusTicks only for
// periodic status logging. Verification is driven by the detection cadence
// through the supplied postPoll callback.
func (c *Coordinator) RunLoopWithCadence(ctx context.Context, detectionTicks, statusTicks <-chan time.Time, refresh func() ([]runtime.Pane, error), postPoll func(now time.Time), logw io.Writer) {
	if logw == nil {
		logw = io.Discard
	}
	if postPoll == nil {
		postPoll = c.postPoll
	}
	lastAction, hadAction := c.LastAction()
	lastFailure, hadFailure := c.LastFailure()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-detectionTicks:
			if !ok {
				return
			}

			panes, err := refresh()
			if err != nil {
				fmt.Fprintf(logw, "%s refresh failed: %v\n", c.clock().Format(time.RFC3339), err)
				continue
			}
			c.SetPanes(panes)
			c.Poll()
			if postPoll != nil {
				postPoll(c.clock())
			}

			failure, hasFailure := c.LastFailure()
			if hasFailure && (!hadFailure || failure.Time != lastFailure.Time || failure.PaneID != lastFailure.PaneID || failure.Err.Error() != lastFailure.Err.Error()) {
				lastFailure = failure
				hadFailure = true
				fmt.Fprintf(logw, "%s pane=%s resume send failed: %v\n", failure.Time.Format(time.RFC3339), failure.PaneID, failure.Err)
				_ = c.rt.Notify("auto-resume", fmt.Sprintf("resume send failed pane %s: %v", failure.PaneID, failure.Err))
			}

			action, ok := c.LastAction()
			if !ok || (hadAction && action == lastAction) {
				continue
			}
			lastAction = action
			hadAction = true
			marker := ""
			if action.DryRun {
				marker = " DRY-RUN"
			}
			fmt.Fprintf(logw, "%s pane=%s kind=%s%s\n", action.Time.Format(time.RFC3339), action.PaneID, action.Kind, marker)
			if !action.DryRun {
				_ = c.rt.Notify("auto-resume", fmt.Sprintf("continued pane %s", action.PaneID))
			}
		case tick, ok := <-statusTicks:
			if !ok {
				statusTicks = nil
				continue
			}
			fmt.Fprintf(logw, "%s status: panes=%d\n", tick.Format(time.RFC3339), len(c.paneOrder))
		}
	}
}
