package jobs

import (
	"fmt"
	"strings"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func (m *Manager) validate(index int, job store.Job, now time.Time) {
	finish := func(state store.JobState, result string, notify bool) {
		job.State = state
		job.LastValidation = result
		if !m.updateJob(index, job) {
			return
		}
		if state == store.StateManualRequired || state == store.StateSessionGone {
			providerName := job.Provider
			if providerName == "" {
				providerName = m.cfg.Provider
			}
			m.logf("limit diagnostic pane=%s job=%s provider=%s reason=%s", job.PaneID, job.ID, providerName, resumeDiagnosticReason(result))
		}
		if notify {
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
	if current == nil && m.cfg.AnswerLimitMenu && strings.TrimSpace(candidate.Agent) == "" && strings.EqualFold(job.Provider, "claude") && looksLikeLimitMenu(content) {
		// A bare Claude limit menu hides the provider chrome. This rescue is
		// deliberately local to a stored Claude menu job; shared identity
		// detection and registry resolution remain unchanged.
		current = m.providers.Resolve(job.Provider, content)
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
	if job.Source == "transient" {
		analysis := detection.Analyze(content, now)
		if analysis.IsLimited && !analysis.Reset.ParsedTime.IsZero() {
			finish(store.StateManualRequired, "reset-bearing limit superseded transient retry", true)
			return
		}
	}
	if job.TerminalID != "" && candidate.TerminalID != job.TerminalID {
		finish(store.StateManualRequired, "pane identity changed", true)
		return
	}
	menuVisible := m.cfg.AnswerLimitMenu && current.Name() == "claude" && looksLikeLimitMenu(content)
	identityFromHint := strings.TrimSpace(candidate.Agent) != "" && strings.EqualFold(candidate.Agent, expectedProvider)
	if !current.DetectContent(content) && !(job.Source == "transient" && identityFromHint) && !menuVisible {
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
	if job.Source == "transient" && current.Name() == "claude" && detection.HasRateLimitMenu(content) {
		finish(store.StateManualRequired, "menu-visible: transient retry requires a non-menu screen", true)
		return
	}
	menuAnswered := false
	if m.cfg.AnswerLimitMenu && current.Name() == "claude" && looksLikeLimitMenu(content) {
		if result, handled := m.answerLimitMenu(index, job, candidate, now); handled {
			for _, persisted := range m.file.Jobs {
				if persisted.ID == job.ID {
					job.MenuAttempt = persisted.MenuAttempt
					break
				}
			}
			if !strings.Contains(result, "menu gone") {
				job.State = store.StateManualRequired
				job.LastValidation = result
				_ = m.updateJob(index, job)
				return
			}
			fresh, err := m.rt.ReadPane(job.PaneID, m.cfg.ReadLines)
			if err != nil {
				finish(store.StateManualRequired, "menu answered; pane reread failed: "+err.Error(), true)
				return
			}
			if detection.HasRateLimitMenu(fresh) {
				finish(store.StateManualRequired, "menu answered; menu still present", true)
				return
			}
			current = m.providers.Resolve(candidate.Agent, fresh)
			if current == nil {
				finish(store.StateManualRequired, "menu answered; pane no longer resolves as "+expectedProvider, true)
				return
			}
			if !strings.EqualFold(current.Name(), expectedProvider) {
				finish(store.StateManualRequired, fmt.Sprintf("menu answered; provider mismatch: job %q, current pane %q", expectedProvider, current.Name()), true)
				return
			}
			content = fresh
			menuAnswered = true
		} else {
			// An interactive-looking menu that fails the strict guard remains
			// manual; it must never fall through to the normal resume action.
			finish(store.StateManualRequired, "menu-visible: terminal menu failed strict answer guard", true)
			return
		}
	}
	if job.Source == "transient" {
		safety, ok := current.(provider.TransientRetrySafety)
		if !ok {
			finish(store.StateManualRequired, "provider has no transient retry safety gate", true)
			return
		}
		if ok, reason := safety.SafeToRetryTransient(content, now); !ok {
			finish(store.StateManualRequired, reason, true)
			return
		}
	} else {
		m.capturePaneContent(job.ID, content, now)
		if ok, reason := current.SafeToResume(content, now); !ok {
			if menuAnswered {
				finish(store.StateManualRequired, "menu answered; pane busy, resume suppressed: "+reason, true)
				return
			}
			finish(store.StateManualRequired, reason, true)
			return
		}
	}
	job.LastValidation = "validation passed"
	if m.updateJob(index, job) {
		m.beginResume(index, job, now, current)
	}
}

func (m *Manager) answerLimitMenu(index int, job store.Job, pane runtime.Pane, now time.Time) (string, bool) {
	if job.MenuAttempt != nil {
		return "menu answer already attempted", true
	}
	if pane.AgentSessionID == "" || job.Episode == "" {
		m.logf("job=%s menu answer refused: missing session or episode identity", job.ID)
		return "", false
	}
	fresh, err := m.rt.ReadPane(job.PaneID, m.cfg.ReadLines)
	if err != nil || !safeStopAndWaitMenu(fresh) {
		return "", false
	}
	m.capturePaneContent(job.ID, fresh, now)
	attempt := &store.MenuAttempt{
		SessionID: pane.AgentSessionID, EpisodeID: job.Episode,
		PaneID: pane.ID, AttemptedAt: now.UTC(),
	}
	recorded, err := m.recordMenuAttempt(job.ID, attempt)
	if err != nil {
		return "menu answer persistence failed", true
	}
	if !recorded {
		return "menu answer already attempted", true
	}
	if m.cfg.DryRun {
		m.logf("job=%s pane=%s menu answer dry-run; no key sent", job.ID, pane.ID)
		return "menu answer dry-run; no key sent", true
	}

	// TOCTOU caveat: Herdr has no revision-conditional send. The pane can
	// change between this guarded read and the unconditional Enter, which is
	// why this feature is default-off and strictly single-shot.
	if err := m.rt.SendKeys(job.PaneID, "enter"); err != nil {
		m.logf("job=%s pane=%s menu answer send failed: %v", job.ID, pane.ID, err)
		return "menu answer send failed", true
	}
	m.sleep(100 * time.Millisecond)
	after, readErr := m.rt.ReadPane(job.PaneID, m.cfg.ReadLines)
	outcome := "menu still present"
	if readErr == nil && !detection.HasRateLimitMenu(after) {
		outcome = "menu gone"
	}
	m.logf("job=%s pane=%s menu answer outcome: %s", job.ID, pane.ID, outcome)
	return "menu answer sent; " + outcome, true
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
		if job.Source == "transient" {
			analysis = detection.Analyze(content, now)
			if analysis.IsLimited && !analysis.Reset.ParsedTime.IsZero() {
				job.State = store.StateManualRequired
				job.LastError = "reset-bearing limit superseded transient retry"
				job.LastValidation = "reset-bearing limit superseded transient retry"
				if m.updateJob(index, job) {
					m.logf("limit diagnostic pane=%s job=%s provider=%s reason=reset-bearing-limit-superseded-transient", job.PaneID, job.ID, job.Provider)
				}
				return
			}
			if !analysis.Transient {
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
				m.scheduleNextTransientRetry(index, job, now)
				return
			}
			job.LastValidation = "transient retry verification pending"
			_ = m.updateJob(index, job)
			return
		}
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

func (m *Manager) scheduleNextTransientRetry(index int, job store.Job, now time.Time) {
	nextAttempt := job.RetryAttempt + 1
	if nextAttempt > m.cfg.TransientMaxAttempts {
		job.State = store.StateManualRequired
		job.LastError = fmt.Sprintf("transient retry attempt limit reached after %d attempts", m.cfg.TransientMaxAttempts)
		job.LastValidation = "transient retry parked at attempt limit"
		if m.updateJob(index, job) {
			m.logf("limit diagnostic pane=%s job=%s provider=%s reason=transient-retry-attempt-limit", job.PaneID, job.ID, job.Provider)
		}
		return
	}
	delay := transientBackoff(nextAttempt)
	job.State = store.StateWaiting
	job.RetryAttempt = nextAttempt
	job.ResumeAtUTC = now.Add(delay).UTC()
	job.Attempts = 0
	job.AttemptID = ""
	job.AttemptAtUTC = time.Time{}
	job.VerifyDeadlineUTC = time.Time{}
	job.LastValidation = "transient retry scheduled"
	if m.updateJob(index, job) {
		m.logf("transient retry pane=%s attempt=%d next_delay=%s class=%s", job.PaneID, nextAttempt, delay, job.TransientClass)
	}
}

func resumeDiagnosticReason(reason string) string {
	var b strings.Builder
	separator := false
	for _, r := range strings.ToLower(strings.TrimSpace(reason)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			separator = false
			continue
		}
		if b.Len() > 0 && !separator {
			b.WriteByte('-')
			separator = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}
