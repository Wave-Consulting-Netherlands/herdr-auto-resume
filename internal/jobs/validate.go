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
	found := false
	for _, candidate := range panes {
		if candidate.ID == job.PaneID {
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
	status := detection.CheckRateLimitAt(content, now)
	analysis := detection.Analyze(content, now)
	idle := detection.IsIdlePrompt(content)
	if analysis.MenuVisible || (!status.IsLimited && !idle) {
		finish(store.StateManualRequired, "terminal is not in a safe blocked or idle state", true)
		return
	}
	job.LastValidation = "validation passed"
	if m.updateJob(index, job) {
		m.beginResume(index, job, now)
	}
}

func (m *Manager) verify(index int, job store.Job, now time.Time) {
	content, err := m.rt.ReadPane(job.PaneID, m.cfg.ReadLines)
	if err == nil {
		status := detection.CheckRateLimitAt(content, now)
		if now.Before(job.VerifyDeadlineUTC) && (!status.IsLimited || hashContent(content) != job.EvidenceHash) {
			job.State = store.StateResumed
			job.LastValidation = "resume verified"
			job.LastError = ""
			if m.updateJob(index, job) {
				m.logf("job=%s active", job.ID)
				m.notify("auto-resume", fmt.Sprintf("job %s resumed", job.ID), false)
			}
			return
		}
		if !now.Before(job.VerifyDeadlineUTC) {
			job.State = store.StateFailed
			job.LastError = "resume verification deadline exceeded"
			job.LastValidation = "resume verification failed"
			if m.updateJob(index, job) {
				m.logf("job=%s failed: %s", job.ID, job.LastError)
				m.notify("auto-resume", fmt.Sprintf("job %s failed: %s", job.ID, job.LastError), true)
			}
			return
		}
		job.LastValidation = "resume verification pending"
	} else {
		if !now.Before(job.VerifyDeadlineUTC) {
			job.State = store.StateFailed
			job.LastError = "resume verification deadline exceeded"
			job.LastValidation = "resume verification failed"
			if m.updateJob(index, job) {
				m.logf("job=%s failed: %s", job.ID, job.LastError)
				m.notify("auto-resume", fmt.Sprintf("job %s failed: %s", job.ID, job.LastError), true)
			}
			return
		}
		job.LastValidation = "resume verification read failed: " + err.Error()
	}
	_ = m.updateJob(index, job)
}
