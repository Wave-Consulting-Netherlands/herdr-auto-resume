package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/walt-verweij/herdr-auto-resume/internal/coordinator"
	runtimeapi "github.com/walt-verweij/herdr-auto-resume/internal/runtime"
	herdradapter "github.com/walt-verweij/herdr-auto-resume/internal/runtime/herdr"
	tmuxadapter "github.com/walt-verweij/herdr-auto-resume/internal/runtime/tmux"
)

type stringList []string

func (s *stringList) String() string {
	return fmt.Sprint([]string(*s))
}

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type runConfig struct {
	Runtime     string
	Panes       []string
	Interval    time.Duration
	Lines       int
	DryRun      bool
	TestPattern string
	HerdrBin    string
	Socket      string
	Session     string
	Workspace   string
}

func parseRunFlags(args []string, stderr io.Writer) (runConfig, error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: herdr-auto-resume run --pane <pane-id> [options]")
		fs.PrintDefaults()
	}
	var panes stringList
	var cfg runConfig
	fs.StringVar(&cfg.Runtime, "runtime", "herdr", "runtime adapter: tmux or herdr")
	fs.Var(&panes, "pane", "pane ID to monitor (repeatable; required)")
	fs.DurationVar(&cfg.Interval, "interval", 3*time.Second, "poll interval")
	fs.IntVar(&cfg.Lines, "lines", 200, "number of recent pane lines to read")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "record automatic continuations without sending them")
	fs.StringVar(&cfg.TestPattern, "test-pattern", "", "trigger auto-continue when this string is found")
	fs.StringVar(&cfg.HerdrBin, "herdr-bin", "herdr", "herdr binary path")
	fs.StringVar(&cfg.Socket, "socket", "", "herdr socket path")
	fs.StringVar(&cfg.Session, "session", "", "herdr session")
	fs.StringVar(&cfg.Workspace, "workspace", "", "herdr workspace")
	if err := fs.Parse(args); err != nil {
		fs.Usage()
		return runConfig{}, err
	}
	if len(panes) == 0 {
		err := errors.New("at least one --pane is required")
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return runConfig{}, err
	}
	if cfg.Runtime != "herdr" && cfg.Runtime != "tmux" {
		err := fmt.Errorf("unsupported runtime %q", cfg.Runtime)
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return runConfig{}, err
	}
	if cfg.Interval <= 0 {
		err := errors.New("--interval must be positive")
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return runConfig{}, err
	}
	cfg.Panes = append([]string(nil), panes...)
	return cfg, nil
}

func filterPanes(panes []runtimeapi.Pane, requested []string, ownPaneID string) ([]runtimeapi.Pane, bool) {
	wanted := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		wanted[id] = struct{}{}
	}
	filtered := make([]runtimeapi.Pane, 0, len(panes))
	excludedOwn := false
	for _, pane := range panes {
		if _, ok := wanted[pane.ID]; !ok {
			continue
		}
		if ownPaneID != "" && pane.ID == ownPaneID {
			excludedOwn = true
			continue
		}
		filtered = append(filtered, pane)
	}
	return filtered, excludedOwn
}

func runtimeForRun(cfg runConfig) (runtimeapi.Runtime, error) {
	if cfg.Runtime == "tmux" {
		return tmuxadapter.New()
	}
	return herdradapter.New(herdradapter.Options{
		Bin:        cfg.HerdrBin,
		SocketPath: cfg.Socket,
		Session:    cfg.Session,
		Workspace:  cfg.Workspace,
	}), nil
}

func runCommand(args []string, _, stderr io.Writer) int {
	cfg, err := parseRunFlags(args, stderr)
	if err != nil {
		return 2
	}
	rt, err := runtimeForRun(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	selfPaneID, err := rt.SelfPaneID()
	if err != nil {
		fmt.Fprintf(stderr, "Error: get own pane: %v\n", err)
		return 1
	}
	allPanes, err := rt.ListPanes()
	if err != nil {
		fmt.Fprintf(stderr, "Error: list panes: %v\n", err)
		return 1
	}
	panes, excludedOwn := filterPanes(allPanes, cfg.Panes, selfPaneID)
	if excludedOwn {
		fmt.Fprintf(stderr, "warning: excluding own pane %s\n", selfPaneID)
	}
	if len(panes) == 0 {
		fmt.Fprintln(stderr, "Error: none of the requested panes were found")
		return 1
	}

	coord := coordinator.New(rt, coordinator.Config{
		OwnPaneID:   selfPaneID,
		TestPattern: cfg.TestPattern,
		DryRun:      cfg.DryRun,
		ReadLines:   cfg.Lines,
	})
	coord.SetPanes(panes)
	coord.Poll()
	coord.EnableAll()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	coord.RunLoop(ctx, ticker.C, func() ([]runtimeapi.Pane, error) {
		all, err := rt.ListPanes()
		if err != nil {
			return nil, err
		}
		filtered, _ := filterPanes(all, cfg.Panes, selfPaneID)
		return filtered, nil
	}, stderr)
	return 0
}
