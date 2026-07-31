package jobs

import (
	"fmt"
	"strings"
	"time"

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
	var candidate runtime.Pane
	found := false
	for _, pane := range panes {
		if pane.ID == job.PaneID {
			candidate = pane
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
	current := m.providers.Resolve(candidate.Agent, content)
	expectedProvider := job.Provider
	if expectedProvider == "" {
		expectedProvider = m.cfg.Provider
	}
	if current == nil {
		reason := fmt.Sprintf("unknown current provider for pane")
		if strings.TrimSpace(candidate.Agent) != "" {
			reason = fmt.Sprintf("pane agent hint %q conflicts with job provider %q", candidate.Agent, expectedProvider)
		}
		finish(store.StateManualRequired, reason, true)
		return
	}
	if !strings.EqualFold(current.Name(), expectedProvider) {
		finish(store.StateManualRequired, fmt.Sprintf("provider mismatch: job %q, current pane %q", expectedProvider, current.Name()), true)
		return
	}
	if !current.DetectContent(content) {
		finish(store.StateManualRequired, "pane is not "+current.Name(), true)
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
	if ok, reason := current.SafeToResume(content, now); !ok {
		finish(store.StateManualRequired, reason, true)
		return
	}
	job.LastValidation = "validation passed"
	if m.updateJob(index, job) {
		m.beginResume(index, job, now, current)
	}
}

func (m *Manager) verify(index int, job store.Job, now time.Time) {
	content, err := m.rt.ReadPane(job.PaneID, m.cfg.ReadLines)
	if err == nil {
		current := m.providerForJob(job)
		if current == nil {
			job.State = store.StateManualRequired
			job.LastError = "unknown provider: " + job.Provider
			job.LastValidation = "unknown provider"
			_ = m.updateJob(index, job)
			return
		}
		analysis := current.Analyze(content, now)
		if now.Before(job.VerifyDeadlineUTC) && (!analysis.IsLimited || (analysis.IsLimited && !analysis.Actionable)) {
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
