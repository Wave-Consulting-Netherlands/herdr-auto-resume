package store

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// JSONStore stores File as one atomically replaced JSON document.
type JSONStore struct {
	path string
}

// NewJSONStore returns a file-backed Store at path.
func NewJSONStore(path string) *JSONStore {
	return &JSONStore{path: path}
}

func (s *JSONStore) Path() string { return s.path }

// WithLock serializes transactions across watcher and CLI processes using a
// sidecar lock file next to state.json. syscall.Flock is supported on the
// Linux and macOS target platforms.
func (s *JSONStore) WithLock(fn func() error) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.path+".lock", os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return fn()
}

// DefaultPath returns the per-user state path, honoring XDG_STATE_HOME.
func DefaultPath() string {
	if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
		return filepath.Join(stateHome, "herdr-auto-resume", "state.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".local", "state", "herdr-auto-resume", "state.json")
	}
	return filepath.Join(home, ".local", "state", "herdr-auto-resume", "state.json")
}

func (s *JSONStore) Load() (File, error) {
	data, err := os.ReadFile(s.path)
	if errorsIsNotExist(err) {
		return emptyFile(), nil
	}
	if err != nil {
		return File{}, err
	}

	var state File
	if err := json.Unmarshal(data, &state); err == nil {
		if state.Version == 0 {
			state.Version = 1
		}
		return state, nil
	} else {
		backupPath := fmt.Sprintf("%s.corrupt-%s", s.path, time.Now().UTC().Format("20060102T150405.000000000Z"))
		if renameErr := os.Rename(s.path, backupPath); renameErr != nil {
			return File{}, CorruptError{BackupPath: backupPath, Err: fmt.Errorf("decode state: %w; backup: %v", err, renameErr)}
		}
		empty := emptyFile()
		if saveErr := s.Save(empty); saveErr != nil {
			return empty, CorruptError{BackupPath: backupPath, Err: fmt.Errorf("decode state: %w; recover: %v", err, saveErr)}
		}
		return empty, CorruptError{BackupPath: backupPath, Err: err}
	}
}

func (s *JSONStore) Save(state File) error {
	if state.Version == 0 {
		state.Version = 1
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := fmt.Sprintf("%s.tmp.%d", s.path, os.Getpid())
	file, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(tmpPath)

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return err
	}
	if err := syncDirectory(dir); err != nil {
		return err
	}
	return nil
}

func emptyFile() File { return File{Version: 1, Jobs: []Job{}} }

func errorsIsNotExist(err error) bool { return err != nil && os.IsNotExist(err) }

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil && err != io.EOF {
		// Directory fsync is best effort across filesystems.
		return nil
	}
	return nil
}
