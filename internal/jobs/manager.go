// Package jobs owns persistent resume-job lifecycle and safety gates.
package jobs

import (
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/coordinator"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/sessionfile"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/terminal"
)

// LimitEvent is re-exported for callers that only need the jobs boundary.
type LimitEvent = coordinator.LimitEvent

// Config controls scheduling and validation.
type Config struct {
	Provider      string
	Margin        time.Duration
	MaxHorizon    time.Duration
	VerifyTimeout time.Duration
	ReadLines     int
	DryRun        bool
}

type Option func(*Manager)

func WithClock(clock func() time.Time) Option {
	return func(m *Manager) {
		if clock != nil {
			m.clock = clock
		}
	}
}

func WithSleep(sleep func(time.Duration)) Option {
	return func(m *Manager) {
		if sleep != nil {
			m.sleep = sleep
		}
	}
}

func WithIDGenerator(next func() string) Option {
	return func(m *Manager) {
		if next != nil {
			m.nextID = next
		}
	}
}

func WithLogWriter(logw io.Writer) Option {
	return func(m *Manager) {
		if logw == nil {
			m.logw = io.Discard
			return
		}
		m.logw = logw
	}
}

func WithProviders(registry *provider.Registry) Option {
	return func(m *Manager) {
		if registry != nil {
			m.providers = registry
		}
	}
}

func WithEpisodeRegistry(registry *sessionfile.EpisodeRegistry) Option {
	return func(m *Manager) { m.episodeRegistry = registry }
}

type Manager struct {
	rt              runtime.Runtime
	store           store.Store
	cfg             Config
	clock           func() time.Time
	sleep           func(time.Duration)
	nextID          func() string
	logw            io.Writer
	file            store.File
	lastMod         time.Time
	providers       *provider.Registry
	episodeRegistry *sessionfile.EpisodeRegistry
}

func defaultRegistry() *provider.Registry {
	return provider.NewRegistry(claude.New(""))
}

func New(rt runtime.Runtime, st store.Store, cfg Config, opts ...Option) *Manager {
	cfg = withDefaults(cfg)
	m := &Manager{
		rt:        rt,
		store:     st,
		cfg:       cfg,
		clock:     time.Now,
		sleep:     time.Sleep,
		nextID:    uuidv4,
		logw:      io.Discard,
		file:      store.File{Version: 1, Jobs: []store.Job{}},
		providers: defaultRegistry(),
	}
	for _, opt := range opts {
		opt(m)
	}
	var loaded store.File
	var loadErr error
	_ = store.WithLock(st, func() error {
		loaded, loadErr = st.Load()
		return nil
	})
	if loadErr == nil || loaded.Version != 0 {
		if loaded.Version == 0 {
			loaded.Version = 1
		}
		if loaded.Jobs == nil {
			loaded.Jobs = []store.Job{}
		}
		m.file = loaded
	} else {
		m.logf("load state: %v", loadErr)
	}
	m.noteModTime()
	return m
}

func withDefaults(cfg Config) Config {
	if cfg.Provider == "" {
		cfg.Provider = "claude"
	}
	if cfg.Margin < 0 {
		cfg.Margin = time.Minute
	}
	if cfg.MaxHorizon <= 0 {
		cfg.MaxHorizon = 192 * time.Hour
	}
	if cfg.VerifyTimeout <= 0 {
		cfg.VerifyTimeout = 90 * time.Second
	}
	if cfg.ReadLines <= 0 {
		cfg.ReadLines = 200
	}
	return cfg
}

// Snapshot returns a copy suitable for status and tests.
func (m *Manager) Snapshot() []store.Job {
	jobs := make([]store.Job, len(m.file.Jobs))
	copy(jobs, m.file.Jobs)
	return jobs
}

// EpisodeIDs returns durable episode mirrors for startup sidecar reconciliation.
func (m *Manager) EpisodeIDs() map[string]struct{} {
	ids := make(map[string]struct{})
	for _, job := range m.file.Jobs {
		if job.Episode != "" {
			ids[job.Episode] = struct{}{}
		}
	}
	return ids
}

// HandleLimit implements coordinator.JobSink. It is level-triggered and owns a
// known-reset episode even when persistence fails only after the attempt to save.
func (m *Manager) HandleLimit(event LimitEvent) bool {
	if m.episodeRegistry != nil && event.EpisodeID == "" && event.Pane.AgentSessionID != "" && !event.ResetTime.IsZero() {
		observation := sessionfile.SessionObservation{
			Provider: event.Provider, SessionID: event.Pane.AgentSessionID,
			ObservedAt: event.ObservedAt, ResetRaw: event.ResetsRaw, ResetAt: event.ResetTime,
		}
		if observation.Provider == "" {
			observation.Provider = m.cfg.Provider
		}
		episode, duplicate, err := m.episodeRegistry.Resolve(observation)
		if err != nil {
			m.logf("episode registry: %v", err)
			return false
		}
		event.EpisodeID = episode.ID
		if duplicate {
			return true
		}
	}
	owned := false
	if err := store.WithLock(m.store, func() error {
		m.reloadLocked()
		owned = m.handleLimitLocked(event)
		return nil
	}); err != nil {
		m.logf("state transaction: %v", err)
		return false
	}
	return owned
}

func (m *Manager) handleLimitLocked(event LimitEvent) bool {
	if event.ResetTime.IsZero() {
		return false
	}
	now := event.ObservedAt
	if now.IsZero() {
		now = m.clock()
	}
	providerName := event.Provider
	if providerName == "" {
		providerName = m.cfg.Provider
	}
	source := event.Source
	if source == "" {
		source = "scrape"
	}
	evidence := event.Evidence
	if evidence == "" {
		if current := m.providers.Resolve(event.Pane.Agent, event.Content); current != nil {
			evidence = current.Analyze(event.Content, now).Evidence
		}
	}
	evidenceHash := hashEvidence(evidence)
	for _, job := range m.file.Jobs {
		if event.EpisodeID != "" && job.Episode == event.EpisodeID {
			return true
		}
		if job.PaneID != event.Pane.ID {
			continue
		}
		if !job.State.Terminal() || job.State != store.StateResumed || job.EvidenceHash == evidenceHash {
			return true
		}
	}

	resetAt := event.ResetTime.UTC()
	resumeAt := resetAt.Add(m.cfg.Margin)
	job := store.Job{
		ID:            m.nextID(),
		Provider:      providerName,
		PaneID:        event.Pane.ID,
		TerminalID:    event.Pane.TerminalID,
		Workspace:     event.Pane.WorkspaceID,
		Agent:         event.Pane.Agent,
		DetectedAt:    now.UTC(),
		RawReset:      event.ResetsRaw,
		ResetKind:     string(event.Spec.Kind),
		ResetTimezone: event.Spec.Timezone,
		Confidence:    string(event.Spec.Confidence),
		ResetAtUTC:    resetAt,
		ResumeAtUTC:   resumeAt,
		MarginSecs:    int64(m.cfg.Margin / time.Second),
		State:         store.StateWaiting,
		EvidenceHash:  evidenceHash,
		EvidenceAtUTC: now.UTC(),
		DryRun:        m.cfg.DryRun,
		Episode:       event.EpisodeID,
		Source:        source,
	}
	if info, err := m.rt.ProcessInfo(event.Pane.ID); err == nil {
		job.ProcCommand = info.Command
		job.WorkingDir = info.CWD
	}
	if resumeAt.Sub(now) > m.cfg.MaxHorizon {
		job.State = store.StateFailed
		job.LastError = fmt.Sprintf("resume time exceeds maximum horizon of %s", m.cfg.MaxHorizon)
	}

	next := append(append([]store.Job(nil), m.file.Jobs...), job)
	if err := m.store.Save(store.File{Version: 1, Jobs: next}); err != nil {
		m.logf("save job %s: %v", job.ID, err)
		return false
	}
	m.file = store.File{Version: 1, Jobs: next}
	m.noteModTime()
	if job.State == store.StateFailed {
		m.logf("job=%s provider=%s failed: %s", job.ID, job.Provider, job.LastError)
		m.notify("auto-resume", fmt.Sprintf("job %s failed: %s", job.ID, job.LastError), true)
	} else {
		m.logf("job=%s provider=%s scheduled pane=%s resume=%s kind=%s confidence=%s", job.ID, job.Provider, job.PaneID, job.ResumeAtUTC.Format(time.RFC3339), job.ResetKind, job.Confidence)
		localReset := event.ResetTime.In(now.Location()).Format("2006-01-02 15:04 MST")
		m.notify("auto-resume", fmt.Sprintf("job %s scheduled for pane %s; reset at %s", job.ID, job.PaneID, localReset), false)
	}
	return true
}

// Tick advances waiting jobs and performs validation when their scheduled time arrives.
func (m *Manager) Tick(now time.Time) {
	m.reloadIfChanged()
	for i := range m.file.Jobs {
		job := m.file.Jobs[i]
		if job.State.Terminal() {
			continue
		}
		if job.State == store.StateWaiting {
			if now.Before(job.ResumeAtUTC) {
				continue
			}
			job.State = store.StateValidating
			if !m.updateJob(i, job) {
				continue
			}
		}
		if job.State == store.StateValidating {
			m.validate(i, job, now)
		} else if job.State == store.StateVerifyingResume {
			m.verify(i, job, now)
		}
	}
}

// Reconcile reloads durable state and makes an interrupted send explicitly
// manual-required. It never retries an uncertain RESUMING job.
func (m *Manager) Reconcile() error {
	return store.WithLock(m.store, m.reconcileLocked)
}

// ReassignPane applies a pane-moved event as one complete state transaction.
// Durable terminal identity wins over the ephemeral public pane id; legacy jobs
// are updated only when the old pane id identifies exactly one active job.
func (m *Manager) ReassignPane(previousPaneID string, pane runtime.Pane) error {
	return store.WithLock(m.store, func() error {
		loaded, err := m.store.Load()
		if err != nil {
			return err
		}
		normalizeFile(&loaded)
		matches := 0
		for _, job := range loaded.Jobs {
			if job.PaneID == previousPaneID && !job.State.Terminal() {
				matches++
			}
		}
		changed := false
		for i, job := range loaded.Jobs {
			if job.PaneID != previousPaneID || job.State.Terminal() {
				continue
			}
			if job.TerminalID == "" && matches != 1 {
				job.State = store.StateManualRequired
				job.LastError = "pane identity ambiguous after move"
				job.LastValidation = "pane identity ambiguous"
				loaded.Jobs[i] = job
				changed = true
				continue
			}
			if job.TerminalID != "" && job.TerminalID != pane.TerminalID {
				job.State = store.StateManualRequired
				job.LastError = "pane identity changed"
				job.LastValidation = "pane identity changed"
				loaded.Jobs[i] = job
				changed = true
				continue
			}
			job.PaneID = pane.ID
			if pane.WorkspaceID != "" {
				job.Workspace = pane.WorkspaceID
			}
			loaded.Jobs[i] = job
			changed = true
		}
		m.file = loaded
		m.noteModTime()
		if !changed {
			return nil
		}
		if err := m.store.Save(loaded); err != nil {
			return err
		}
		m.noteModTime()
		return nil
	})
}

// ReconcilePanes repairs missing public ids when a snapshot contains exactly
// one pane with a job's durable terminal identity. Ambiguous and legacy cases
// remain untouched for the normal validation path to classify safely.
func (m *Manager) ReconcilePanes(panes []runtime.Pane) error {
	return store.WithLock(m.store, func() error {
		loaded, err := m.store.Load()
		if err != nil {
			return err
		}
		normalizeFile(&loaded)
		changed := false
		for i, job := range loaded.Jobs {
			if job.State.Terminal() || job.TerminalID == "" {
				continue
			}
			present := false
			matches := make([]runtime.Pane, 0, 1)
			for _, pane := range panes {
				if pane.ID == job.PaneID {
					present = true
				}
				if pane.TerminalID == job.TerminalID {
					matches = append(matches, pane)
				}
			}
			if present || len(matches) != 1 {
				continue
			}
			job.PaneID = matches[0].ID
			if matches[0].WorkspaceID != "" {
				job.Workspace = matches[0].WorkspaceID
			}
			loaded.Jobs[i] = job
			changed = true
		}
		m.file = loaded
		m.noteModTime()
		if !changed {
			return nil
		}
		if err := m.store.Save(loaded); err != nil {
			return err
		}
		m.noteModTime()
		return nil
	})
}

func (m *Manager) reconcileLocked() error {
	loaded, err := m.store.Load()
	var corrupt store.CorruptError
	if err != nil && !errors.As(err, &corrupt) {
		return err
	}
	normalizeFile(&loaded)
	m.file = loaded
	m.noteModTime()
	if err != nil {
		m.logf("state recovery warning: %v", err)
		return nil
	}

	now := m.clock()
	changed := false
	for i := range m.file.Jobs {
		job := m.file.Jobs[i]
		switch {
		case job.State == store.StateResuming:
			job.State = store.StateManualRequired
			job.LastError = "watcher restarted during uncertain resume send"
			job.LastValidation = "reconciled as manual-required"
			m.file.Jobs[i] = job
			changed = true
			m.notify("auto-resume", fmt.Sprintf("job %s requires manual intervention after restart", job.ID), false)
		case job.State == store.StateWaiting && job.ResumeAtUTC.Sub(now) > m.cfg.MaxHorizon:
			job.State = store.StateFailed
			job.LastError = fmt.Sprintf("resume time exceeds maximum horizon of %s", m.cfg.MaxHorizon)
			m.file.Jobs[i] = job
			changed = true
			m.notify("auto-resume", fmt.Sprintf("job %s failed: %s", job.ID, job.LastError), true)
		}
	}
	if changed {
		if err := m.store.Save(m.file); err != nil {
			return err
		}
		m.noteModTime()
	}
	return nil
}

func (m *Manager) noteModTime() {
	path := m.store.Path()
	if path == "" {
		m.lastMod = time.Time{}
		return
	}
	info, err := os.Stat(path)
	if err == nil {
		m.lastMod = info.ModTime()
	}
}

func (m *Manager) reloadIfChanged() {
	_ = store.WithLock(m.store, func() error {
		m.reloadLocked()
		return nil
	})
}

func (m *Manager) reloadLocked() {
	path := m.store.Path()
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil || (!m.lastMod.IsZero() && info.ModTime().Equal(m.lastMod)) {
		return
	}
	loaded, loadErr := m.store.Load()
	if loaded.Version == 0 {
		loaded.Version = 1
	}
	if loaded.Jobs == nil {
		loaded.Jobs = []store.Job{}
	}
	if loadErr != nil {
		m.logf("reload state: %v", loadErr)
	}
	m.file = loaded
	m.noteModTime()
}

func (m *Manager) beginResume(index int, job store.Job, now time.Time, current provider.Provider) {
	job.State = store.StateResuming
	job.AttemptID = m.nextID()
	job.AttemptAtUTC = now.UTC()
	job.Attempts = 1
	if !m.updateJob(index, job) {
		return
	}

	if m.cfg.DryRun {
		job.State = store.StateResumed
		job.LastValidation = "dry-run resume recorded"
		_ = m.updateJob(index, job)
		m.logf("job=%s dry-run resume", job.ID)
		return
	}
	if err := m.sendResumeIfCurrent(index, job, current); err != nil {
		if errors.Is(err, errConcurrentState) {
			return
		}
		job.State = store.StateManualRequired
		job.LastError = "resume send failed: " + err.Error()
		job.LastValidation = "resume send failed"
		if m.updateJob(index, job) {
			m.notify("auto-resume", fmt.Sprintf("job %s requires manual intervention: %s", job.ID, job.LastError), false)
		}
		return
	}
	job.State = store.StateVerifyingResume
	job.VerifyDeadlineUTC = now.Add(m.cfg.VerifyTimeout).UTC()
	job.LastValidation = "resume submitted; verifying"
	if m.updateJob(index, job) {
		m.logf("job=%s resume submitted", job.ID)
		m.notify("auto-resume", fmt.Sprintf("job %s submitted; verifying", job.ID), false)
	}
}

var errConcurrentState = errors.New("job state changed concurrently")

func (m *Manager) sendResumeIfCurrent(index int, job store.Job, current provider.Provider) error {
	if index < 0 || index >= len(m.file.Jobs) {
		return errConcurrentState
	}
	expectedID := m.file.Jobs[index].ID
	var sendErr error
	err := store.WithLock(m.store, func() error {
		loaded, err := m.store.Load()
		if err != nil {
			return err
		}
		normalizeFile(&loaded)
		loaded.Jobs = append([]store.Job(nil), loaded.Jobs...)
		found := -1
		for i := range loaded.Jobs {
			if loaded.Jobs[i].ID == expectedID {
				found = i
				break
			}
		}
		if found < 0 || loaded.Jobs[found].State != store.StateResuming {
			m.file = loaded
			m.noteModTime()
			return errConcurrentState
		}
		m.file = loaded
		m.noteModTime()
		sendErr = coordinator.SendResumeAction(m.rt, loaded.Jobs[found].PaneID, current.ResumeAction(), m.sleep)
		if sendErr == nil {
			return nil
		}
		failed := loaded.Jobs[found]
		failed.State = store.StateManualRequired
		failed.LastError = "resume send failed: " + sendErr.Error()
		failed.LastValidation = "resume send failed"
		loaded.Jobs[found] = failed
		if err := m.store.Save(loaded); err != nil {
			return fmt.Errorf("record resume send failure: %w", err)
		}
		m.file = loaded
		m.noteModTime()
		return sendErr
	})
	if err != nil {
		return err
	}
	return sendErr
}

func (m *Manager) providerForJob(job store.Job) provider.Provider {
	name := job.Provider
	if name == "" {
		name = m.cfg.Provider
	}
	return m.providers.Resolve(name, "")
}

func (m *Manager) updateJob(index int, job store.Job) bool {
	if index < 0 || index >= len(m.file.Jobs) {
		return false
	}
	expectedID := m.file.Jobs[index].ID
	expectedState := m.file.Jobs[index].State
	updated := false
	err := store.WithLock(m.store, func() error {
		loaded, err := m.store.Load()
		if err != nil {
			return err
		}
		normalizeFile(&loaded)
		loaded.Jobs = append([]store.Job(nil), loaded.Jobs...)
		found := -1
		for i := range loaded.Jobs {
			if loaded.Jobs[i].ID == expectedID {
				found = i
				break
			}
		}
		if found < 0 || loaded.Jobs[found].State != expectedState {
			m.file = loaded
			m.noteModTime()
			return errConcurrentState
		}
		loaded.Jobs[found] = job
		if err := m.store.Save(loaded); err != nil {
			return err
		}
		m.file = loaded
		m.noteModTime()
		updated = true
		return nil
	})
	if err != nil {
		m.logf("save job %s: %v", job.ID, err)
		return false
	}
	return updated
}

func normalizeFile(file *store.File) {
	if file.Version == 0 {
		file.Version = 1
	}
	if file.Jobs == nil {
		file.Jobs = []store.Job{}
	}
}

func (m *Manager) notify(title, body string, failed bool) {
	if m.cfg.DryRun {
		return
	}
	if err := m.rt.Notify(title, body); err != nil {
		m.logf("notify job%s: %v", map[bool]string{true: " failure", false: ""}[failed], err)
	}
}

func (m *Manager) logf(format string, args ...any) {
	fmt.Fprintf(m.logw, format+"\n", args...)
}

func hashEvidence(evidence string) string {
	lines := strings.Split(terminal.StripANSI(evidence), "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			normalized = append(normalized, line)
		}
	}
	hash := sha256.Sum256([]byte(strings.Join(normalized, "\n")))
	return hex.EncodeToString(hash[:])
}

var fallbackID uint64

func uuidv4() string {
	var bytes [16]byte
	if _, err := cryptorand.Read(bytes[:]); err != nil {
		counter := atomic.AddUint64(&fallbackID, 1)
		for i := range bytes {
			bytes[i] = byte(counter >> ((i % 8) * 8))
		}
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
