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
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
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

type Manager struct {
	rt      runtime.Runtime
	store   store.Store
	cfg     Config
	clock   func() time.Time
	sleep   func(time.Duration)
	nextID  func() string
	logw    io.Writer
	file    store.File
	lastMod time.Time
}

func New(rt runtime.Runtime, st store.Store, cfg Config, opts ...Option) *Manager {
	cfg = withDefaults(cfg)
	m := &Manager{
		rt:     rt,
		store:  st,
		cfg:    cfg,
		clock:  time.Now,
		sleep:  time.Sleep,
		nextID: uuidv4,
		logw:   io.Discard,
		file:   store.File{Version: 1, Jobs: []store.Job{}},
	}
	for _, opt := range opts {
		opt(m)
	}
	if loaded, err := st.Load(); err == nil || loaded.Version != 0 {
		if loaded.Version == 0 {
			loaded.Version = 1
		}
		if loaded.Jobs == nil {
			loaded.Jobs = []store.Job{}
		}
		m.file = loaded
	} else {
		m.logf("load state: %v", err)
	}
	m.noteModTime()
	return m
}

func withDefaults(cfg Config) Config {
	if cfg.Provider == "" {
		cfg.Provider = "claude"
	}
	if cfg.Margin <= 0 {
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

// HandleLimit implements coordinator.JobSink. It is level-triggered and owns a
// known-reset episode even when persistence fails only after the attempt to save.
func (m *Manager) HandleLimit(event LimitEvent) bool {
	m.reloadIfChanged()
	if event.ResetTime.IsZero() {
		return false
	}
	now := event.ObservedAt
	if now.IsZero() {
		now = m.clock()
	}
	evidenceHash := hashContent(event.Content)
	for _, job := range m.file.Jobs {
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
		Provider:      m.cfg.Provider,
		PaneID:        event.Pane.ID,
		Agent:         event.Pane.Agent,
		DetectedAt:    now.UTC(),
		RawReset:      event.ResetsRaw,
		ResetAtUTC:    resetAt,
		ResumeAtUTC:   resumeAt,
		MarginSecs:    int64(m.cfg.Margin / time.Second),
		State:         store.StateWaiting,
		EvidenceHash:  evidenceHash,
		EvidenceAtUTC: now.UTC(),
		DryRun:        m.cfg.DryRun,
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
		m.logf("job=%s failed: %s", job.ID, job.LastError)
		m.notify("auto-resume", fmt.Sprintf("job %s failed: %s", job.ID, job.LastError), true)
	} else {
		m.logf("job=%s scheduled pane=%s resume=%s", job.ID, job.PaneID, job.ResumeAtUTC.Format(time.RFC3339))
		m.notify("auto-resume", fmt.Sprintf("job %s scheduled for pane %s", job.ID, job.PaneID), false)
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
	loaded, err := m.store.Load()
	var corrupt store.CorruptError
	if err != nil && !errors.As(err, &corrupt) {
		return err
	}
	if loaded.Version == 0 {
		loaded.Version = 1
	}
	if loaded.Jobs == nil {
		loaded.Jobs = []store.Job{}
	}
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

func (m *Manager) beginResume(index int, job store.Job, now time.Time) {
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
	if err := coordinator.SendContinueSequence(m.rt, job.PaneID, m.sleep); err != nil {
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

func (m *Manager) updateJob(index int, job store.Job) bool {
	next := append([]store.Job(nil), m.file.Jobs...)
	next[index] = job
	if err := m.store.Save(store.File{Version: 1, Jobs: next}); err != nil {
		m.logf("save job %s: %v", job.ID, err)
		return false
	}
	m.file = store.File{Version: 1, Jobs: next}
	m.noteModTime()
	return true
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

func hashContent(content string) string {
	hash := sha256.Sum256([]byte(content))
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

func hasMenuInTail(content string) bool {
	lines := strings.Split(content, "\n")
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	for _, line := range lines {
		if strings.Contains(line, "❯") {
			return true
		}
	}
	return false
}
