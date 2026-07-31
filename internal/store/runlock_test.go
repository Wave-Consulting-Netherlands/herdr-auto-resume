package store

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunLockRejectsSecondAcquireWithHolderPID(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	first, err := AcquireRunLock(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	_, err = AcquireRunLock(statePath)
	if err == nil || !strings.Contains(err.Error(), filepath.Clean(statePath)) || !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) {
		t.Fatalf("second acquire error = %v, want state path and holder PID", err)
	}
}

func TestRunLockDifferentPathsCanBeHeld(t *testing.T) {
	first, err := AcquireRunLock(filepath.Join(t.TempDir(), "one.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	second, err := AcquireRunLock(filepath.Join(t.TempDir(), "two.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
}

func TestRunLockReleaseAllowsReacquireAndWritesPID(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	first, err := AcquireRunLock(statePath)
	if err != nil {
		t.Fatal(err)
	}
	lockPath, err := filepath.Abs(statePath + ".run")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil || strings.TrimSpace(string(data)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("run lock contents = %q, err=%v, want PID %d", data, err, os.Getpid())
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireRunLock(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}
