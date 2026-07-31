// Package store persists scheduled resume jobs.
package store

import "time"

// JobState is the durable lifecycle state of a resume job.
type JobState string

const (
	StateWaiting         JobState = "WAITING"
	StateValidating      JobState = "VALIDATING"
	StateResuming        JobState = "RESUMING"
	StateVerifyingResume JobState = "VERIFYING_RESUME"
	StateResumed         JobState = "RESUMED"
	StateManualRequired  JobState = "MANUAL_REQUIRED"
	StateFailed          JobState = "FAILED"
	StateCancelled       JobState = "CANCELLED"
	StateDisabled        JobState = "DISABLED"
	StateSessionGone     JobState = "SESSION_GONE"
)

// Terminal reports whether no further scheduler work should be performed for a state.
func (s JobState) Terminal() bool {
	switch s {
	case StateResumed, StateManualRequired, StateFailed, StateCancelled, StateDisabled, StateSessionGone:
		return true
	default:
		return false
	}
}

// Job is a durable scheduled continuation and its validation/attempt evidence.
type Job struct {
	ID                string    `json:"id"`
	Provider          string    `json:"provider"`
	PaneID            string    `json:"pane_id"`
	Workspace         string    `json:"workspace"`
	Agent             string    `json:"agent"`
	ProcCommand       string    `json:"proc_command"`
	WorkingDir        string    `json:"working_dir"`
	DetectedAt        time.Time `json:"detected_at"`
	RawReset          string    `json:"raw_reset"`
	ResetKind         string    `json:"reset_kind,omitempty"`
	ResetTimezone     string    `json:"reset_timezone,omitempty"`
	Confidence        string    `json:"confidence,omitempty"`
	ResetAtUTC        time.Time `json:"reset_at_utc"`
	ResumeAtUTC       time.Time `json:"resume_at_utc"`
	MarginSecs        int64     `json:"margin_secs"`
	State             JobState  `json:"state"`
	Attempts          int       `json:"attempts"`
	AttemptID         string    `json:"attempt_id"`
	AttemptAtUTC      time.Time `json:"attempt_at_utc"`
	VerifyDeadlineUTC time.Time `json:"verify_deadline_utc"`
	LastValidation    string    `json:"last_validation"`
	LastError         string    `json:"last_error"`
	EvidenceHash      string    `json:"evidence_hash"`
	EvidenceAtUTC     time.Time `json:"evidence_at_utc"`
	DryRun            bool      `json:"dry_run"`
}

// File is the complete on-disk state document.
type File struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}

// Store is the persistence boundary used by the scheduler and CLI commands.
type Store interface {
	Load() (File, error)
	Save(File) error
	Path() string
}

// CorruptError reports that the previous state was invalid but has been backed up
// and replaced with an empty usable state file.
type CorruptError struct {
	BackupPath string
	Err        error
}

func (e CorruptError) Error() string {
	return "corrupt state backed up to " + e.BackupPath + ": " + e.Err.Error()
}

func (e CorruptError) Unwrap() error { return e.Err }
