package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/coordinator"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/jobs"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/claude"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/provider/codex"
	runtimeapi "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	herdradapter "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/herdr"
	tmuxadapter "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/tmux"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
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
	Runtime       string
	Panes         []string
	Interval      time.Duration
	Lines         int
	DryRun        bool
	TestPattern   string
	HerdrBin      string
	Socket        string
	Session       string
	Workspace     string
	StateFile     string
	Margin        time.Duration
	MaxWait       time.Duration
	VerifyTimeout time.Duration
	Providers     string
	ClaudePrompt  string
	CodexPrompt   string
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
	fs.StringVar(&cfg.StateFile, "state-file", "auto", "persistent job state path, auto, or off")
	fs.DurationVar(&cfg.Margin, "margin", time.Minute, "safety margin after reset")
	fs.DurationVar(&cfg.MaxWait, "max-wait", 192*time.Hour, "maximum scheduled wait horizon")
	fs.DurationVar(&cfg.VerifyTimeout, "verify-timeout", 90*time.Second, "resume verification timeout")
	fs.StringVar(&cfg.Providers, "providers", "claude,codex", "enabled providers: claude,codex")
	fs.StringVar(&cfg.ClaudePrompt, "claude-prompt", claude.New("").ResumeAction().Text, "Claude continuation prompt")
	fs.StringVar(&cfg.CodexPrompt, "codex-prompt", codex.New("").ResumeAction().Text, "Codex continuation prompt")
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
	if cfg.Margin < 0 || cfg.MaxWait <= 0 || cfg.VerifyTimeout <= 0 {
		err := errors.New("--margin must be non-negative; --max-wait and --verify-timeout must be positive")
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return runConfig{}, err
	}
	if _, err := buildProviderRegistry(cfg.Providers, cfg.ClaudePrompt, cfg.CodexPrompt); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return runConfig{}, err
	}
	cfg.Panes = append([]string(nil), panes...)
	return cfg, nil
}

func buildProviderRegistry(names, claudePrompt, codexPrompt string) (*provider.Registry, error) {
	var enabled []provider.Provider
	seen := make(map[string]bool)
	for _, raw := range strings.Split(names, ",") {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			return nil, errors.New("unsupported provider \"\"")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		switch name {
		case "claude":
			enabled = append(enabled, claude.New(claudePrompt))
		case "codex":
			enabled = append(enabled, codex.New(codexPrompt))
		default:
			return nil, fmt.Errorf("unsupported provider %q", name)
		}
	}
	if len(enabled) == 0 {
		return nil, errors.New("at least one provider is required")
	}
	return provider.NewRegistry(enabled...), nil
}

func resolveStatePath(cfg runConfig) string {
	if cfg.StateFile == "off" || (cfg.StateFile == "auto" && cfg.Runtime == "tmux") {
		return "off"
	}
	if cfg.StateFile == "" || cfg.StateFile == "auto" {
		return store.DefaultPath()
	}
	return cfg.StateFile
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
	registry, err := buildProviderRegistry(cfg.Providers, cfg.ClaudePrompt, cfg.CodexPrompt)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
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
	statePath := resolveStatePath(cfg)
	fmt.Fprintf(stderr, "state path: %s\n", statePath)
	coordOpts := make([]coordinator.Option, 0, 2)
	if statePath != "off" {
		st := store.NewJSONStore(statePath)
		manager := jobs.New(rt, st, jobs.Config{
			Provider:      "claude",
			Margin:        cfg.Margin,
			MaxHorizon:    cfg.MaxWait,
			VerifyTimeout: cfg.VerifyTimeout,
			ReadLines:     cfg.Lines,
			DryRun:        cfg.DryRun,
		}, jobs.WithLogWriter(stderr), jobs.WithProviders(registry))
		if err := manager.Reconcile(); err != nil {
			fmt.Fprintf(stderr, "Error: reconcile state: %v\n", err)
			return 1
		}
		coordOpts = append(coordOpts,
			coordinator.WithJobSink(manager),
			coordinator.WithPostPoll(manager.Tick),
		)
	}
	coordOpts = append(coordOpts, coordinator.WithProviders(registry))

	coord := coordinator.New(rt, coordinator.Config{
		OwnPaneID:   selfPaneID,
		TestPattern: cfg.TestPattern,
		DryRun:      cfg.DryRun,
		ReadLines:   cfg.Lines,
	}, coordOpts...)
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
