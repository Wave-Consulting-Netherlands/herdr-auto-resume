package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	tmuxadapter "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/tmux"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/tui"
)

var version = "dev"

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
		case "version":
			fmt.Fprintln(stdout, version)
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
