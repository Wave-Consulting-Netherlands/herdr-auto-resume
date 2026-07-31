package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	appconfig "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/config"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

func jobCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: herdr-auto-resume status|inspect|cancel [id-prefix] [--state-file path]")
		return 2
	}
	subcommand := args[0]
	positionals, flagArgs := splitJobArgs(args[1:])
	fs := flag.NewFlagSet(subcommand, flag.ContinueOnError)
	fs.SetOutput(stderr)
	statePath := fs.String("state-file", store.DefaultPath(), "state file path")
	configPath := fs.String("config", appconfig.DefaultPath(), "configuration file path")
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	stateFileSet := false
	configExplicit := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "state-file":
			stateFileSet = true
		case "config":
			configExplicit = true
		}
	})
	if !stateFileSet {
		fileConfig, found, err := appconfig.Load(*configPath)
		if err != nil {
			fmt.Fprintf(stderr, "error: load config: %v\n", err)
			return 2
		}
		if configExplicit && !found {
			fmt.Fprintf(stderr, "error: config file %s was not found\n", *configPath)
			return 2
		}
		if found && fileConfig.Has("state.file") {
			*statePath = fileConfig.State.File
			if *statePath == "auto" {
				*statePath = store.DefaultPath()
			}
		}
	}
	if *statePath == "off" {
		fmt.Fprintln(stderr, "error: jobs require an enabled --state-file")
		return 2
	}
	if len(positionals) > 1 || (subcommand != "status" && len(positionals) != 1) || (subcommand == "status" && len(positionals) != 0) {
		fmt.Fprintln(stderr, "error: invalid job command arguments")
		return 2
	}
	st := store.NewJSONStore(*statePath)
	var file store.File
	var err error
	lockErr := store.WithLock(st, func() error {
		file, err = st.Load()
		return nil
	})
	if lockErr != nil {
		fmt.Fprintf(stderr, "error: lock state: %v\n", lockErr)
		return 1
	}
	var corrupt store.CorruptError
	if err != nil && !errors.As(err, &corrupt) {
		fmt.Fprintf(stderr, "error: load state: %v\n", err)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "warning: %v\n", err)
	}

	switch subcommand {
	case "status":
		writeJobStatus(stdout, file.Jobs, time.Local)
		return 0
	case "inspect":
		return inspectJob(positionals[0], file.Jobs, stdout, stderr)
	case "cancel":
		return cancelJob(positionals[0], file, st, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown job subcommand: %s\n", subcommand)
		return 2
	}
}

func splitJobArgs(args []string) (positionals, flags []string) {
	for i := 0; i < len(args); i++ {
		if (args[i] == "--state-file" || args[i] == "--config") && i+1 < len(args) {
			flags = append(flags, args[i], args[i+1])
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--state-file=") || strings.HasPrefix(args[i], "--config=") {
			flags = append(flags, args[i])
			continue
		}
		positionals = append(positionals, args[i])
	}
	return positionals, flags
}

func writeJobStatus(out io.Writer, jobs []store.Job, loc *time.Location) {
	if loc == nil {
		loc = time.Local
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "JOB\tPANE\tSTATE\tRESET(local)\tRESUME(UTC)\tATTEMPTS\tERROR\tPROVIDER")
	for _, job := range jobs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n", shortJobID(job.ID), job.PaneID, job.State, displayTime(job.ResetAtUTC.In(loc)), displayTime(job.ResumeAtUTC.UTC()), job.Attempts, job.LastError, job.Provider)
	}
	_ = w.Flush()
}

func shortJobID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func displayTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format(time.RFC3339)
}

func findJob(prefix string, jobs []store.Job) (*store.Job, error) {
	var matches []store.Job
	for _, job := range jobs {
		if strings.HasPrefix(job.ID, prefix) {
			matches = append(matches, job)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no job matches %q", prefix)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("ambiguous job prefix %q", prefix)
	}
	job := matches[0]
	return &job, nil
}

func inspectJob(prefix string, jobs []store.Job, stdout, stderr io.Writer) int {
	job, err := findJob(prefix, jobs)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(job); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func cancelJob(prefix string, file store.File, st store.Store, stdout, stderr io.Writer) int {
	var cancelledID string
	err := store.WithLock(st, func() error {
		fresh, err := st.Load()
		if err != nil {
			return err
		}
		job, err := findJob(prefix, fresh.Jobs)
		if err != nil {
			return err
		}
		if job.State.Terminal() {
			return fmt.Errorf("job %s is terminal (%s)", job.ID, job.State)
		}
		for i := range fresh.Jobs {
			if fresh.Jobs[i].ID == job.ID {
				fresh.Jobs[i].State = store.StateCancelled
				fresh.Jobs[i].LastValidation = "cancelled by user"
			}
		}
		if err := st.Save(fresh); err != nil {
			return err
		}
		cancelledID = job.ID
		return nil
	})
	if err != nil {
		fmt.Fprintln(stderr, "error: save state:", err)
		return 1
	}
	fmt.Fprintf(stdout, "cancelled %s\n", cancelledID)
	return 0
}
