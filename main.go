package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	tmuxadapter "github.com/walt-verweij/herdr-auto-resume/internal/runtime/tmux"
	"github.com/walt-verweij/herdr-auto-resume/internal/tui"
)

var version = "dev"

func main() {
	testPattern := flag.String("test-pattern", "", "Test mode: trigger auto-continue when this string is found (for debugging)")
	dryRun := flag.Bool("dry-run", false, "Record automatic continuations without sending them")
	flag.Parse()

	rt, err := tmuxadapter.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
