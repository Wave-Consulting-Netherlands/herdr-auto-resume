package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	_ "time/tzdata"

	tmuxadapter "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/tmux"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"
var commit = "none"
var date = "unknown"

func versionInfo() (string, string, string) {
	currentVersion, currentCommit, currentDate := version, commit, date
	if currentVersion == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					if setting.Value != "" {
						currentCommit = setting.Value
					}
				case "vcs.time":
					if setting.Value != "" {
						currentDate = setting.Value
					}
				}
			}
		}
	}
	return currentVersion, currentCommit, currentDate
}

func versionOutput() string {
	currentVersion, currentCommit, currentDate := versionInfo()
	return fmt.Sprintf("herdr-auto-resume %s (commit %s, built %s, %s)", currentVersion, currentCommit, currentDate, runtime.Version())
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "run":
			return runCommand(args[1:], stdout, stderr)
		case "doctor":
			return doctorCommand(args[1:], stdout, stderr)
		case "detect":
			return detectCommand(args[1:], stdout, stderr)
		case "status", "inspect", "cancel":
			return jobCommand(args, stdout, stderr)
		case "version":
			fmt.Fprintln(stdout, versionOutput())
			return 0
		}
		if args[0][0] != '-' {
			fmt.Fprintf(stderr, "unknown subcommand: %s\n", args[0])
			return 2
		}
	}

	return runTUI(args, stderr, stdout)
}

func runTUI(args []string, stderr, _ io.Writer) int {
	fs := flag.NewFlagSet("herdr-auto-resume", flag.ContinueOnError)
	fs.SetOutput(stderr)
	testPattern := fs.String("test-pattern", "", "Test mode: trigger auto-continue when this string is found (for debugging)")
	dryRun := fs.Bool("dry-run", false, "Record automatic continuations without sending them")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	rt, err := tmuxadapter.New()
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	p := tea.NewProgram(
		tui.New(version, *testPattern, rt, *dryRun),
		tea.WithAltScreen(),
	)

	// Handle SIGINT and SIGTERM to ensure clean exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}
