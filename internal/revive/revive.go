// Package revive implements the one-shot Claude session revive operator.
// It has no watcher loop and never sends a continuation after attaching.
package revive

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	runtimeapi "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/sessionfile"
)

const defaultReviveGrace = time.Minute

// PaneRuntime is deliberately limited to the fresh, unfiltered snapshot
// needed by the operator command.
type PaneRuntime interface {
	ListPanes() ([]runtimeapi.Pane, error)
}

// Workspace is the result needed to run Claude in the newly created pane.
type Workspace struct {
	WorkspaceID string
	PaneID      string
}

type Spawner interface {
	CreateWorkspace(label, cwd string) (Workspace, error)
	RunPane(paneID string, args ...string) error
}

type Config struct {
	Scanner       *sessionfile.Scanner
	Runtime       PaneRuntime
	Spawner       Spawner
	Now           func() time.Time
	Sleep         func(time.Duration)
	Grace         time.Duration
	AttachTimeout time.Duration
	PollInterval  time.Duration
	LeasePID      int
	Log           io.Writer
}

type Operator struct {
	config Config
}

func New(config Config) *Operator {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Sleep == nil {
		config.Sleep = time.Sleep
	}
	if config.Grace <= 0 {
		config.Grace = defaultReviveGrace
	}
	if config.AttachTimeout <= 0 {
		config.AttachTimeout = 10 * time.Second
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 100 * time.Millisecond
	}
	if config.LeasePID <= 0 {
		config.LeasePID = os.Getpid()
	}
	if config.Log == nil {
		config.Log = io.Discard
	}
	return &Operator{config: config}
}

// Run resolves a unique session, holds its lease for the complete operation,
// vetoes double attachment, persists ATTACHING before spawning, and records
// ATTACHED only after the new pane reports the expected session identity.
func (o *Operator) Run(prefix string, output io.Writer) error {
	if o.config.Scanner == nil || o.config.Runtime == nil || o.config.Spawner == nil {
		return errors.New("revive operator is missing scanner, runtime, or spawner")
	}
	if output == nil {
		output = io.Discard
	}
	// Reconciliation is part of every invocation, including an ambiguous or
	// unknown prefix invocation. It repairs crash state before new work.
	if err := o.reconcileStale(""); err != nil {
		return err
	}
	candidates, err := o.config.Scanner.DiscoverSessions(prefix)
	if err != nil {
		return err
	}
	if len(candidates) != 1 {
		all, listErr := o.config.Scanner.DiscoverSessions("")
		if listErr != nil {
			return listErr
		}
		return fmt.Errorf("session prefix %q is not unique; candidates: %s", prefix, formatCandidates(all))
	}
	session := candidates[0]
	if session.CWD == "" {
		return fmt.Errorf("session %s has no working directory", session.SessionID)
	}
	lease, err := AcquireSessionLease(o.config.Scanner.StatePath(), session.SessionID, o.config.LeasePID)
	if err != nil {
		return err
	}
	defer lease.Release()

	if err := o.reconcileStale(session.SessionID); err != nil {
		return err
	}
	if err := o.reconcileTarget(session.SessionID); err != nil {
		return err
	}
	panes, err := o.config.Runtime.ListPanes()
	if err != nil {
		return fmt.Errorf("snapshot panes before revive: %w", err)
	}
	if pane, ok := paneForSession(panes, session.SessionID); ok {
		return fmt.Errorf("session is attached to pane %s; answer/resume it there", pane.ID)
	}
	if err := o.config.Scanner.BeginRevive(sessionfile.ReviveIntent{
		SessionID: session.SessionID,
		Timestamp: o.config.Now(),
		LeasePID:  o.config.LeasePID,
		State:     sessionfile.ReviveAttaching,
	}); err != nil {
		return err
	}
	// The lease excludes another revive, but not a user or watcher attaching
	// the session. Re-check after durable intent and immediately before spawn
	// to close that avoidable check-then-act window.
	panes, err = o.config.Runtime.ListPanes()
	if err != nil {
		return fmt.Errorf("snapshot panes before revive spawn: %w", err)
	}
	if pane, ok := paneForSession(panes, session.SessionID); ok {
		if completeErr := o.config.Scanner.CompleteRevive(session.SessionID, pane.ID, pane.WorkspaceID); completeErr != nil {
			return completeErr
		}
		return fmt.Errorf("session attached to pane %s before revive spawn; no new pane created", pane.ID)
	}
	workspace, err := o.config.Spawner.CreateWorkspace("herdr-auto-resume-revive", session.CWD)
	if err != nil {
		return fmt.Errorf("create revive workspace: %w", err)
	}
	if workspace.PaneID == "" {
		return errors.New("create revive workspace returned no pane id")
	}
	if err := o.config.Spawner.RunPane(workspace.PaneID, "claude", "--resume", session.SessionID); err != nil {
		return fmt.Errorf("start Claude in revive pane: %w", err)
	}
	pane, err := o.waitForAttachment(session.SessionID)
	if err != nil {
		return err
	}
	if err := o.config.Scanner.CompleteRevive(session.SessionID, pane.ID, pane.WorkspaceID); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "revived session %s in pane %s, workspace %s; no continue was sent (normal watcher detection handles the resumed pane once monitored)\n", session.SessionID, pane.ID, workspace.WorkspaceID)
	return nil
}

// Reconcile repairs stale ATTACHING records on every operator invocation.
func (o *Operator) Reconcile() error {
	return o.reconcileStale("")
}

func (o *Operator) reconcileStale(skipSession string) error {
	intents, err := o.config.Scanner.ReviveIntents()
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if intent.State != sessionfile.ReviveAttaching || intent.SessionID == skipSession || o.config.Now().Sub(intent.Timestamp) < o.config.Grace {
			continue
		}
		lease, err := AcquireSessionLease(o.config.Scanner.StatePath(), intent.SessionID, o.config.LeasePID)
		if err != nil {
			continue
		}
		err = o.reconcileOne(intent)
		lease.Release()
		if err != nil {
			return err
		}
	}
	return nil
}

func (o *Operator) reconcileTarget(sessionID string) error {
	intents, err := o.config.Scanner.ReviveIntents()
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if intent.SessionID != sessionID {
			continue
		}
		if intent.State == sessionfile.ReviveAttaching {
			if o.config.Now().Sub(intent.Timestamp) < o.config.Grace {
				return fmt.Errorf("revive for session %s is already attaching", sessionID)
			}
			return o.reconcileOne(intent)
		}
	}
	return nil
}

func (o *Operator) reconcileOne(intent sessionfile.ReviveIntent) error {
	panes, err := o.config.Runtime.ListPanes()
	if err != nil {
		return fmt.Errorf("snapshot panes while reconciling session %s: %w", intent.SessionID, err)
	}
	if pane, ok := paneForSession(panes, intent.SessionID); ok {
		if err := o.config.Scanner.CompleteRevive(intent.SessionID, pane.ID, pane.WorkspaceID); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(o.config.Log, "reconciled revive session %s as ATTACHED to pane %s\n", intent.SessionID, pane.ID)
		return nil
	}
	if err := o.config.Scanner.ClearRevive(intent.SessionID); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(o.config.Log, "cleared stale revive intent for session %s; no pane was attached\n", intent.SessionID)
	return nil
}

func (o *Operator) waitForAttachment(sessionID string) (runtimeapi.Pane, error) {
	deadline := time.Now().Add(o.config.AttachTimeout)
	for {
		panes, err := o.config.Runtime.ListPanes()
		if err != nil {
			return runtimeapi.Pane{}, fmt.Errorf("snapshot panes after revive: %w", err)
		}
		if pane, ok := paneForSession(panes, sessionID); ok {
			return pane, nil
		}
		if time.Now().After(deadline) {
			return runtimeapi.Pane{}, fmt.Errorf("revive pane did not report session %s before timeout", sessionID)
		}
		o.config.Sleep(o.config.PollInterval)
	}
}

func paneForSession(panes []runtimeapi.Pane, sessionID string) (runtimeapi.Pane, bool) {
	for _, pane := range panes {
		if pane.AgentSessionID == sessionID {
			return pane, true
		}
	}
	return runtimeapi.Pane{}, false
}

func formatCandidates(candidates []sessionfile.SessionInfo) string {
	if len(candidates) == 0 {
		return "(none)"
	}
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.SessionID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ", ")
}

type SessionLease struct {
	file *os.File
}

// AcquireSessionLease uses a non-blocking flock so a second operator fails
// immediately rather than waiting behind the first spawn operation.
func AcquireSessionLease(statePath, sessionID string, pid int) (*SessionLease, error) {
	if statePath == "" || sessionID == "" {
		return nil, errors.New("revive lease requires state path and session")
	}
	path := statePath + ".revive." + sessionID + ".lock"
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open revive lease: %w", err)
	}
	_ = file.Chmod(0o600)
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		holder := readLeasePID(file)
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			if holder > 0 {
				return nil, fmt.Errorf("revive session %s is already in use (holder pid %d)", sessionID, holder)
			}
			return nil, fmt.Errorf("revive session %s is already in use", sessionID)
		}
		return nil, fmt.Errorf("acquire revive lease: %w", err)
	}
	if err := file.Truncate(0); err == nil {
		_, _ = file.WriteString(fmt.Sprintf("%d\n", pid))
		_ = file.Sync()
	}
	return &SessionLease{file: file}, nil
}

func readLeasePID(file *os.File) int {
	if _, err := file.Seek(0, 0); err != nil {
		return 0
	}
	var pid int
	_, _ = fmt.Fscan(file, &pid)
	return pid
}

func (l *SessionLease) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
	l.file = nil
}
