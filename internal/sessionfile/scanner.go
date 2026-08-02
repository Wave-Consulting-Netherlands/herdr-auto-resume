// Package sessionfile scans Claude Code's durable session records for rate
// limit observations. It deliberately has no coordinator or pane-runtime
// dependencies; later phases consume the pending observations from its
// sidecar.
package sessionfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/detection"
)

const (
	ProviderClaude  = "claude"
	ReviveAttaching = "ATTACHING"
	ReviveAttached  = "ATTACHED"
	sidecarVersion  = 1
	defaultLookback = 2 * time.Hour
)

var sessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// SessionObservation is the file-channel representation of a Claude rate
// limit event. It is intentionally not a coordinator LimitEvent: a file
// record has no pane or screen evidence.
type SessionObservation struct {
	Provider   string    `json:"provider"`
	SessionID  string    `json:"session_id"`
	CWD        string    `json:"cwd"`
	ObservedAt time.Time `json:"observed_at"`
	ResetRaw   string    `json:"reset_raw"`
	ResetAt    time.Time `json:"reset_at"`
	RequestID  string    `json:"request_id"`
}

// SessionInfo identifies a Claude session file that can be resumed by the
// operator command.
type SessionInfo struct {
	SessionID string
	CWD       string
}

// ReviveIntent is the crash-recovery record for the one-shot revive command.
// It lives in the shared scan sidecar so all mutations use the scanner flock.
type ReviveIntent struct {
	SessionID   string    `json:"session_id"`
	Timestamp   time.Time `json:"timestamp"`
	LeasePID    int       `json:"lease_pid"`
	State       string    `json:"state"`
	PaneID      string    `json:"pane_id,omitempty"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
}

// Config controls scanner discovery and persistence. RootDir is the Claude
// directory (the directory containing projects/), not the projects directory.
type Config struct {
	RootDir   string
	StatePath string
	// StateFile is retained as a descriptive alias for callers that name the
	// state path after the daemon's state file.
	StateFile string
	Lookback  time.Duration
	Now       func() time.Time
	Clock     func() time.Time
}

// Scanner reads top-level Claude project session files and records accepted
// observations in a durable pending list before moving each file cursor.
type Scanner struct {
	rootDir      string
	statePath    string
	lookback     time.Duration
	now          func() time.Time
	afterPersist func(SessionObservation) error
}

// ResolveEpisode shares the scanner sidecar's episode registry with the
// coordinator and scrape job path.
func (s *Scanner) ResolveEpisode(observation SessionObservation) (Episode, bool, error) {
	registry, err := NewEpisodeRegistry(s.statePath)
	if err != nil {
		return Episode{}, false, err
	}
	return registry.Resolve(observation)
}

// RootDir and StatePath expose immutable scanner roots to the operator layer.
func (s *Scanner) RootDir() string   { return s.rootDir }
func (s *Scanner) StatePath() string { return s.statePath }

// DiscoverSessions returns valid top-level Claude session files matching the
// prefix. An empty prefix returns every candidate in stable order.
func (s *Scanner) DiscoverSessions(prefix string) ([]SessionInfo, error) {
	paths, err := s.discover()
	if err != nil {
		return nil, err
	}
	result := make([]SessionInfo, 0)
	for _, path := range paths {
		id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		if prefix != "" && !strings.HasPrefix(id, prefix) {
			continue
		}
		cwd, err := sessionCWD(path, id)
		if err != nil {
			return nil, err
		}
		result = append(result, SessionInfo{SessionID: id, CWD: cwd})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SessionID < result[j].SessionID })
	return result, nil
}

func sessionCWD(path, sessionID string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read Claude session %s: %w", sessionID, err)
	}
	for _, line := range splitLines(data) {
		var record struct {
			SessionID string `json:"sessionId"`
			CWD       string `json:"cwd"`
		}
		if json.Unmarshal(bytes.TrimSpace(line), &record) == nil && record.SessionID == sessionID && record.CWD != "" {
			return record.CWD, nil
		}
	}
	return "", nil
}

// ReviveIntents returns a copy of the durable revive records.
func (s *Scanner) ReviveIntents() ([]ReviveIntent, error) {
	state, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	result := make([]ReviveIntent, 0, len(state.Revives))
	for _, intent := range state.Revives {
		result = append(result, intent)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].SessionID < result[j].SessionID })
	return result, nil
}

// BeginRevive durably records ATTACHING before any process is spawned.
func (s *Scanner) BeginRevive(intent ReviveIntent) error {
	if intent.SessionID == "" || intent.State != ReviveAttaching {
		return errors.New("revive intent requires a session and ATTACHING state")
	}
	return s.mutate(func(state *sidecar) (bool, error) {
		if existing, ok := state.Revives[intent.SessionID]; ok && existing.State != ReviveAttached {
			return false, fmt.Errorf("revive intent already exists for session %s in state %s", intent.SessionID, existing.State)
		}
		state.Revives[intent.SessionID] = intent
		return true, nil
	})
}

// CompleteRevive transitions ATTACHING to ATTACHED after the new pane is
// visible with the expected agent session.
func (s *Scanner) CompleteRevive(sessionID, paneID, workspaceID string) error {
	return s.mutate(func(state *sidecar) (bool, error) {
		intent, ok := state.Revives[sessionID]
		if !ok {
			return false, fmt.Errorf("revive intent for session %s is missing", sessionID)
		}
		intent.State = ReviveAttached
		intent.PaneID = paneID
		intent.WorkspaceID = workspaceID
		state.Revives[sessionID] = intent
		return true, nil
	})
}

// ClearRevive removes an ATTACHING intent after reconciliation confirms that
// no pane carries the session.
func (s *Scanner) ClearRevive(sessionID string) error {
	return s.mutate(func(state *sidecar) (bool, error) {
		if _, ok := state.Revives[sessionID]; !ok {
			return false, nil
		}
		delete(state.Revives, sessionID)
		return true, nil
	})
}

type sidecar struct {
	Version        int                     `json:"version"`
	Files          map[string]fileCursor   `json:"files"`
	Pending        []SessionObservation    `json:"pending"`
	SeenRequestIDs map[string]bool         `json:"seen_request_ids"`
	Episodes       []episodeRecord         `json:"episodes"`
	Committed      map[string]string       `json:"committed"`
	Revives        map[string]ReviveIntent `json:"revives"`
}

type fileCursor struct {
	Device     uint64 `json:"device"`
	Inode      uint64 `json:"inode"`
	Offset     int64  `json:"offset"`
	PrefixHash string `json:"prefix_hash"`
}

type claudeRecord struct {
	IsSidechain bool   `json:"isSidechain"`
	Timestamp   string `json:"timestamp"`
	SessionID   string `json:"sessionId"`
	CWD         string `json:"cwd"`
	RequestID   string `json:"requestId"`
	Error       string `json:"error"`
	Message     struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// New creates a scanner. A persistent state path is required because the
// cursor contract is explicitly durable and shared across processes.
func New(config Config) (*Scanner, error) {
	root := config.RootDir
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("find Claude home: %w", err)
		}
		root = filepath.Join(home, ".claude")
	}
	state := config.StatePath
	if state == "" {
		state = config.StateFile
	}
	if state == "" {
		return nil, errors.New("sessionfile state path is required")
	}
	lookback := config.Lookback
	if lookback <= 0 {
		lookback = defaultLookback
	}
	now := config.Now
	if now == nil {
		now = config.Clock
	}
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(filepath.Dir(state), 0o700); err != nil {
		return nil, fmt.Errorf("create sessionfile state directory: %w", err)
	}
	return &Scanner{rootDir: root, statePath: state, lookback: lookback, now: now}, nil
}

// Scan returns newly accepted observations. Each returned observation is
// first written to the sidecar pending list; a request ID already present in
// that durable list is not returned again.
func (s *Scanner) Scan() ([]SessionObservation, error) {
	paths, err := s.discover()
	if err != nil {
		return nil, err
	}
	observations := make([]SessionObservation, 0)
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat Claude session %s: %w", path, err)
		}
		identity, ok := fileIdentity(info)
		if !ok {
			continue
		}
		state, err := s.snapshot()
		if err != nil {
			return nil, err
		}
		cursor, exists := state.Files[path]
		start := int64(0)
		bootstrap := !exists
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read Claude session %s: %w", path, err)
		}
		if start > int64(len(data)) {
			start = 0
			bootstrap = false
		}
		if exists && cursor.Device == identity.Device && cursor.Inode == identity.Inode && info.Size() >= cursor.Offset && (cursor.PrefixHash == "" || cursor.PrefixHash == prefixHash(data, cursor.Offset)) {
			start = cursor.Offset
		} else if exists {
			bootstrap = false
		}
		complete := completeLineEnd(data[start:])
		end := start + int64(complete)
		for _, line := range splitLines(data[start:end]) {
			observation, ok := parseRecord(line, filepath.Base(path))
			if !ok || (bootstrap && observation.ObservedAt.Before(s.now().Add(-s.lookback))) {
				continue
			}
			accepted := false
			if err := s.mutate(func(state *sidecar) (bool, error) {
				if state.SeenRequestIDs[observation.RequestID] {
					return false, nil
				}
				state.Pending = append(state.Pending, observation)
				state.SeenRequestIDs[observation.RequestID] = true
				accepted = true
				return true, nil
			}); err != nil {
				return nil, err
			}
			if !accepted {
				continue
			}
			if s.afterPersist != nil {
				if err := s.afterPersist(observation); err != nil {
					return nil, err
				}
			}
			observations = append(observations, observation)
		}
		desired := fileCursor{Device: identity.Device, Inode: identity.Inode, Offset: end, PrefixHash: prefixHash(data, end)}
		if err := s.mutate(func(state *sidecar) (bool, error) {
			current, exists := state.Files[path]
			if exists && current.Device == desired.Device && current.Inode == desired.Inode && current.Offset >= desired.Offset {
				return false, nil
			}
			state.Files[path] = desired
			return true, nil
		}); err != nil {
			return nil, err
		}
	}
	return observations, nil
}

func (s *Scanner) discover() ([]string, error) {
	projects := filepath.Join(s.rootDir, "projects")
	projectEntries, err := os.ReadDir(projects)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read Claude projects: %w", err)
	}
	paths := make([]string, 0)
	for _, project := range projectEntries {
		if !project.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(projects, project.Name()))
		if err != nil {
			return nil, fmt.Errorf("read Claude project %s: %w", project.Name(), err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" || !sessionIDPattern.MatchString(strings.TrimSuffix(entry.Name(), ".jsonl")) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return nil, fmt.Errorf("stat Claude session %s: %w", entry.Name(), err)
			}
			if !info.Mode().IsRegular() {
				continue
			}
			paths = append(paths, filepath.Join(projects, project.Name(), entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func parseRecord(line []byte, filename string) (SessionObservation, bool) {
	var record claudeRecord
	if json.Unmarshal(bytes.TrimSpace(line), &record) != nil || record.Error != "rate_limit" || record.IsSidechain {
		return SessionObservation{}, false
	}
	filenameID := strings.TrimSuffix(filename, ".jsonl")
	if !sessionIDPattern.MatchString(filenameID) || !sessionIDPattern.MatchString(record.SessionID) || record.SessionID != filenameID || record.CWD == "" || record.RequestID == "" {
		return SessionObservation{}, false
	}
	observedAt, err := time.Parse(time.RFC3339Nano, record.Timestamp)
	if err != nil {
		return SessionObservation{}, false
	}
	texts := make([]string, 0, len(record.Message.Content))
	for _, content := range record.Message.Content {
		if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
			texts = append(texts, strings.TrimSpace(content.Text))
		}
	}
	banner := strings.Join(texts, "\n")
	reset := detection.ParseReset(banner, observedAt)
	if reset.ParsedTime.IsZero() {
		return SessionObservation{}, false
	}
	return SessionObservation{Provider: ProviderClaude, SessionID: record.SessionID, CWD: record.CWD, ObservedAt: observedAt, ResetRaw: banner, ResetAt: reset.ParsedTime, RequestID: record.RequestID}, true
}

func splitLines(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	lines := bytes.Split(data, []byte{'\n'})
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func completeLineEnd(data []byte) int {
	index := bytes.LastIndexByte(data, '\n')
	if index < 0 {
		return 0
	}
	return index + 1
}

func fileIdentity(info os.FileInfo) (fileCursor, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileCursor{}, false
	}
	return fileCursor{Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}, true
}

func prefixHash(data []byte, offset int64) string {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	if offset > 4096 {
		offset = 4096
	}
	sum := sha256.Sum256(data[:offset])
	return hex.EncodeToString(sum[:])
}

func (s *Scanner) sidecarPath() string { return s.statePath + ".scan.json" }
func (s *Scanner) lockPath() string    { return s.statePath + ".scan.lock" }

func (s *Scanner) lock() (*os.File, error) {
	file, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open sessionfile scan lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure sessionfile scan lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire sessionfile scan lock: %w", err)
	}
	return file, nil
}

func unlock(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}

// mutate is the sidecar's transaction boundary. The callback is invoked with
// freshly read state while the exclusive flock is held; only the delta it
// applies is written back. Callers must not retain the state after return.
func (s *Scanner) mutate(fn func(*sidecar) (changed bool, err error)) error {
	lock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock(lock)
	state, err := s.readSidecar()
	if err != nil {
		return err
	}
	changed, err := fn(&state)
	if err != nil || !changed {
		return err
	}
	return s.writeSidecar(state)
}

func (s *Scanner) snapshot() (sidecar, error) {
	lock, err := s.lock()
	if err != nil {
		return sidecar{}, err
	}
	defer unlock(lock)
	return s.readSidecar()
}

func (s *Scanner) readSidecar() (sidecar, error) {
	data, err := os.ReadFile(s.sidecarPath())
	if os.IsNotExist(err) {
		return sidecar{Version: sidecarVersion, Files: map[string]fileCursor{}, SeenRequestIDs: map[string]bool{}, Committed: map[string]string{}, Revives: map[string]ReviveIntent{}}, nil
	}
	if err != nil {
		return sidecar{}, fmt.Errorf("read sessionfile sidecar: %w", err)
	}
	var state sidecar
	if err := json.Unmarshal(data, &state); err != nil {
		return sidecar{}, fmt.Errorf("decode sessionfile sidecar: %w", err)
	}
	if state.Version != sidecarVersion {
		return sidecar{}, fmt.Errorf("unsupported sessionfile sidecar version %d", state.Version)
	}
	if state.Files == nil {
		state.Files = map[string]fileCursor{}
	}
	if state.SeenRequestIDs == nil {
		state.SeenRequestIDs = map[string]bool{}
	}
	if state.Committed == nil {
		state.Committed = map[string]string{}
	}
	if state.Revives == nil {
		state.Revives = map[string]ReviveIntent{}
	}
	return state, nil
}

// Pending returns observations durably accepted by Scan but not yet committed
// to a job store.
func (s *Scanner) Pending() ([]SessionObservation, error) {
	state, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	return append([]SessionObservation(nil), state.Pending...), nil
}

// CommitPending records the sidecar COMMITTED state after the job store has
// durably accepted the corresponding job, then removes it from PENDING.
func (s *Scanner) CommitPending(requestID, episodeID string) error {
	return s.mutate(func(state *sidecar) (bool, error) {
		remaining := state.Pending[:0]
		found := false
		for _, observation := range state.Pending {
			if observation.RequestID == requestID {
				found = true
				state.Committed[requestID] = episodeID
				continue
			}
			remaining = append(remaining, observation)
		}
		if !found {
			return false, nil
		}
		state.Pending = remaining
		return true, nil
	})
}

// ReconcilePending commits pending observations whose episode is already
// represented by a durable job. Pending observations without such a job stay
// pending and will be retried on the next coordinator cycle.
func (s *Scanner) ReconcilePending(episodeIDs map[string]struct{}) error {
	return s.mutate(func(state *sidecar) (bool, error) {
		remaining := state.Pending[:0]
		changed := false
		for _, observation := range state.Pending {
			episode, ok := nearestEpisode(state.Episodes, observation)
			if ok {
				if _, exists := episodeIDs[episode.ID]; exists {
					state.Committed[observation.RequestID] = episode.ID
					changed = true
					continue
				}
			}
			remaining = append(remaining, observation)
		}
		if !changed {
			return false, nil
		}
		state.Pending = remaining
		return true, nil
	})
}

func (s *Scanner) writeSidecar(state sidecar) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sessionfile sidecar: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.sidecarPath())
	tmp, err := os.CreateTemp(dir, ".sessionfile-scan-*")
	if err != nil {
		return fmt.Errorf("create sessionfile sidecar temporary: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure sessionfile sidecar temporary: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write sessionfile sidecar: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync sessionfile sidecar: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close sessionfile sidecar temporary: %w", err)
	}
	if err := os.Rename(tmpName, s.sidecarPath()); err != nil {
		return fmt.Errorf("replace sessionfile sidecar: %w", err)
	}
	if err := os.Chmod(s.sidecarPath(), 0o600); err != nil {
		return fmt.Errorf("secure sessionfile sidecar: %w", err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open sessionfile sidecar directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return fmt.Errorf("sync sessionfile sidecar directory: %w", err)
	}
	return nil
}
