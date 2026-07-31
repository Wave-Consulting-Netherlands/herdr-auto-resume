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
	if logw == nil {
		logw = io.Discard
	}
	lastAction, hadAction := c.LastAction()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-ticks:
			if !ok {
				return
			}

			panes, err := refresh()
			if err != nil {
				fmt.Fprintf(logw, "%s refresh failed: %v\n", time.Now().Format(time.RFC3339), err)
				continue
			}
			c.SetPanes(panes)
			c.Poll()
			if c.postPoll != nil {
				c.postPoll(c.clock())
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
		}
	}
}
