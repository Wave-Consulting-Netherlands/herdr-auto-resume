package jobs

import (
	"fmt"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func (m *Manager) validate(index int, job store.Job, now time.Time) {
	finish := func(state store.JobState, result string, notify bool) {
		job.State = state
		job.LastValidation = result
		if m.updateJob(index, job) && notify {
			m.notify("auto-resume", fmt.Sprintf("job %s: %s", job.ID, result), false)
		}
	}

	if job.State == store.StateCancelled || job.State == store.StateDisabled {
		finish(job.State, "cancelled or disabled", false)
		return
	}
	if now.Before(job.ResumeAtUTC) {
		finish(store.StateWaiting, "waiting for scheduled resume time", false)
		return
	}
	if job.Attempts != 0 {
		finish(store.StateManualRequired, "attempt already recorded", true)
		return
	}
	panes, err := m.rt.ListPanes()
	if err != nil {
		job.LastValidation = "runtime unavailable: " + err.Error()
		_ = m.updateJob(index, job)
		return
	}
	var pane runtime.Pane
	found := false
	for _, candidate := range panes {
		if candidate.ID == job.PaneID {
			pane = candidate
			found = true
			break
		}
	}
	if !found {
		finish(store.StateSessionGone, "pane is gone", true)
		return
	}
	content, err := m.rt.ReadPane(job.PaneID, m.cfg.ReadLines)
	if err != nil {
		finish(store.StateManualRequired, "pane read failed: "+err.Error(), true)
		return
	}
	if !detection.IsClaudeCode(content) {
		finish(store.StateManualRequired, "pane is not Claude Code", true)
		return
	}
	info, err := m.rt.ProcessInfo(job.PaneID)
	if err != nil {
		info = runtime.ProcessInfo{}
	}
	if job.ProcCommand != "" && info.Command != job.ProcCommand {
		finish(store.StateManualRequired, "foreground process changed", true)
		return
	}
	if job.WorkingDir != "" && info.CWD != job.WorkingDir {
		finish(store.StateManualRequired, "working directory changed", true)
		return
	}
	status := detection.CheckRateLimit(content)
	idle := detection.IsIdlePrompt(content)
	if hasMenuInTail(content) || (!status.IsLimited && !idle) {
		finish(store.StateManualRequired, "terminal is not in a safe blocked or idle state", true)
		return
	}
	_ = pane
	finish(store.StateValidating, "validation passed", false)
}
