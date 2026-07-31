package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// RunLockHeldError reports the watcher that already owns a state file's run lock.
type RunLockHeldError struct {
	StatePath string
	PID       string
}

func (e *RunLockHeldError) Error() string {
	return fmt.Sprintf("state file %s is already in use by herdr-auto-resume run (pid %s); use a different --state-file for a second watcher", e.StatePath, e.PID)
}

// RunLock is the process-lifetime lock for a watcher using one state file.
type RunLock struct {
	file *os.File
}

// AcquireRunLock takes an exclusive, non-blocking lock on the state sidecar.
func AcquireRunLock(statePath string) (*RunLock, error) {
	absStatePath, err := filepath.Abs(statePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(absStatePath), 0700); err != nil {
		return nil, err
	}
	lockPath := absStatePath + ".run"
	file, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			data, readErr := os.ReadFile(lockPath)
			pid := strings.TrimSpace(string(data))
			if readErr != nil || pid == "" {
				pid = "unknown"
			}
			_ = file.Close()
			return nil, &RunLockHeldError{StatePath: absStatePath, PID: pid}
		}
		_ = file.Close()
		return nil, err
	}
	if err := file.Truncate(0); err != nil {
		_ = file.Close()
		return nil, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		_ = file.Close()
		return nil, err
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &RunLock{file: file}, nil
}

// Release closes the lock descriptor. The sidecar is intentionally retained.
func (l *RunLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
