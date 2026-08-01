package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	appconfig "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/config"
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
	Runtime           string
	Transport         string
	TransportExplicit bool
	SocketExplicit    bool
	Panes             []string
	Interval          time.Duration
	Lines             int
	DryRun            bool
	TestPattern       string
	HerdrBin          string
	Socket            string
	Session           string
	Workspace         string
	StateFile         string
	Margin            time.Duration
	MaxWait           time.Duration
	VerifyTimeout     time.Duration
	WaitForPanes      bool
	Providers         string
	ClaudePrompt      string
	CodexPrompt       string
}

const herdrDetectionMatchRegex = `(?i)(?:limit\s+reached|rate\s+limit|usage\s+limit|out\s+of\s+(?:extra\s+)?usage|try\s+again\s+in|you['’]ve\s+hit\s+your|out\s+of\s+credits|spend\s+cap)`

func parseRunFlags(args []string, stderr io.Writer) (runConfig, error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: herdr-auto-resume run --pane <pane-id> [options]")
		fs.PrintDefaults()
	}
	var panes stringList
	defaults := runConfig{
		Runtime: "herdr", Interval: 3 * time.Second, Lines: 200,
		HerdrBin: "herdr", StateFile: "auto", Margin: time.Minute,
		MaxWait: 192 * time.Hour, VerifyTimeout: 90 * time.Second,
		Providers: "claude,codex", ClaudePrompt: claude.New("").ResumeAction().Text,
		CodexPrompt: codex.New("").ResumeAction().Text,
	}
	cfg := defaults
	configPath := fs.String("config", appconfig.DefaultPath(), "configuration file path")
	fs.StringVar(&cfg.Runtime, "runtime", "herdr", "runtime adapter: tmux or herdr")
	fs.StringVar(&cfg.Transport, "transport", "", "herdr transport: cli or socket")
	fs.Var(&panes, "pane", "pane ID to monitor (repeatable; required)")
	fs.DurationVar(&cfg.Interval, "interval", 3*time.Second, "poll interval")
	fs.IntVar(&cfg.Lines, "lines", 200, "number of recent pane lines to read")
	fs.BoolVar(&cfg.WaitForPanes, "wait-for-panes", false, "wait for requested panes and transient runtime reachability")
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
	fileConfig, found, configErr := appconfig.Load(*configPath)
	if configErr != nil {
		err := fmt.Errorf("load config: %w", configErr)
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return runConfig{}, err
	}
	configExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configExplicit = true
		}
	})
	if configExplicit && !found {
		err := fmt.Errorf("config file %s was not found", *configPath)
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return runConfig{}, err
	}
	parsedCfg := cfg
	cfg = mergeRunConfig(defaults, fileConfig)
	applyExplicitRunFlags(&cfg, &parsedCfg, &panes, fs)
	resolvedTransport, transportErr := resolveTransportDefault(cfg.Runtime, cfg.Transport, cfg.Session, cfg.TransportExplicit, stderr)
	if transportErr != nil {
		fmt.Fprintln(stderr, "error:", transportErr)
		fs.Usage()
		return runConfig{}, transportErr
	}
	cfg.Transport = resolvedTransport
	if len(cfg.Panes) == 0 {
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
	if cfg.Transport != "cli" && cfg.Transport != "socket" {
		err := fmt.Errorf("unsupported transport %q", cfg.Transport)
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return runConfig{}, err
	}
	if cfg.Transport == "socket" && cfg.Runtime != "herdr" {
		err := errors.New("socket transport requires --runtime herdr")
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return runConfig{}, err
	}
	if cfg.Transport == "socket" && cfg.Session != "" {
		err := errors.New("--session is unsupported with socket transport; use --socket")
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
	if cfg.VerifyTimeout < 2*cfg.Interval {
		err := fmt.Errorf("--verify-timeout must be at least twice --interval (%s)", 2*cfg.Interval)
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return runConfig{}, err
	}
	if _, err := buildProviderRegistry(cfg.Providers, cfg.ClaudePrompt, cfg.CodexPrompt); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return runConfig{}, err
	}
	return cfg, nil
}

func mergeRunConfig(defaults runConfig, file appconfig.Config) runConfig {
	merged := defaults
	if file.Has("runtime.type") {
		merged.Runtime = file.Runtime.Type
	}
	if file.Has("runtime.transport") {
		merged.Transport = file.Runtime.Transport
		merged.TransportExplicit = true
	}
	if file.Has("runtime.herdr_bin") {
		merged.HerdrBin = file.Runtime.HerdrBin
	}
	if file.Has("runtime.socket") {
		merged.Socket = file.Runtime.Socket
		merged.SocketExplicit = true
	}
	if file.Has("runtime.workspace") {
		merged.Workspace = file.Runtime.Workspace
	}
	if file.Has("monitoring.panes") {
		merged.Panes = append([]string(nil), file.Monitoring.Panes...)
	}
	if file.Has("monitoring.interval") {
		merged.Interval = file.Monitoring.Interval
	}
	if file.Has("monitoring.lines") {
		merged.Lines = file.Monitoring.Lines
	}
	if file.Has("monitoring.wait_for_panes") {
		merged.WaitForPanes = file.Monitoring.WaitForPanes
	}
	if file.Has("resume.margin") {
		merged.Margin = file.Resume.Margin
	}
	if file.Has("resume.max_wait") {
		merged.MaxWait = file.Resume.MaxWait
	}
	if file.Has("resume.verify_timeout") {
		merged.VerifyTimeout = file.Resume.VerifyTimeout
	}
	if file.Has("providers.enabled") {
		merged.Providers = strings.Join(file.Providers.Enabled, ",")
	}
	if file.Has("providers.claude_prompt") {
		merged.ClaudePrompt = file.Providers.ClaudePrompt
	}
	if file.Has("providers.codex_prompt") {
		merged.CodexPrompt = file.Providers.CodexPrompt
	}
	if file.Has("state.file") {
		merged.StateFile = file.State.File
	}
	return merged
}

func applyExplicitRunFlags(dst, parsed *runConfig, panes *stringList, fs *flag.FlagSet) {
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "runtime":
			dst.Runtime = parsed.Runtime
		case "transport":
			dst.Transport = parsed.Transport
			dst.TransportExplicit = true
		case "pane":
			dst.Panes = append([]string(nil), (*panes)...)
		case "interval":
			dst.Interval = parsed.Interval
		case "lines":
			dst.Lines = parsed.Lines
		case "wait-for-panes":
			dst.WaitForPanes = parsed.WaitForPanes
		case "dry-run":
			dst.DryRun = parsed.DryRun
		case "test-pattern":
			dst.TestPattern = parsed.TestPattern
		case "herdr-bin":
			dst.HerdrBin = parsed.HerdrBin
		case "socket":
			dst.Socket = parsed.Socket
			dst.SocketExplicit = true
		case "session":
			dst.Session = parsed.Session
		case "workspace":
			dst.Workspace = parsed.Workspace
		case "state-file":
			dst.StateFile = parsed.StateFile
		case "margin":
			dst.Margin = parsed.Margin
		case "max-wait":
			dst.MaxWait = parsed.MaxWait
		case "verify-timeout":
			dst.VerifyTimeout = parsed.VerifyTimeout
		case "providers":
			dst.Providers = parsed.Providers
		case "claude-prompt":
			dst.ClaudePrompt = parsed.ClaudePrompt
		case "codex-prompt":
			dst.CodexPrompt = parsed.CodexPrompt
		}
	})
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
	return filterPanesByIdentity(panes, requested, nil, ownPaneID)
}

func filterPanesByIdentity(panes []runtimeapi.Pane, requested []string, terminalIDs map[string]struct{}, ownPaneID string) ([]runtimeapi.Pane, bool) {
	wanted := make(map[string]struct{}, len(requested))
	for _, id := range requested {
		wanted[id] = struct{}{}
	}
	filtered := make([]runtimeapi.Pane, 0, len(panes))
	excludedOwn := false
	for _, pane := range panes {
		_, idWanted := wanted[pane.ID]
		_, terminalWanted := terminalIDs[pane.TerminalID]
		if !idWanted && !terminalWanted {
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

const paneWaitBackoff = 5 * time.Second

func listPanesStartup(ctx context.Context, rt runtimeapi.Runtime, requested []string, ownPaneID string, waitForPanes bool, logw io.Writer) ([]runtimeapi.Pane, error) {
	if waitForPanes {
		return waitForPanesUntilReady(ctx, rt, requested, ownPaneID, logw, sleepForPanes)
	}
	allPanes, err := rt.ListPanes()
	if err != nil {
		return nil, fmt.Errorf("list panes: %w", err)
	}
	panes, excludedOwn := filterPanes(allPanes, requested, ownPaneID)
	if excludedOwn && logw != nil {
		fmt.Fprintf(logw, "warning: excluding own pane %s\n", ownPaneID)
	}
	if len(panes) == 0 {
		return nil, errors.New("none of the requested panes were found")
	}
	return panes, nil
}

func waitForPanes(ctx context.Context, rt runtimeapi.Runtime, requested []string, ownPaneID string, logw io.Writer, wait func(context.Context, time.Duration) error) ([]runtimeapi.Pane, error) {
	return waitForPanesUntilReady(ctx, rt, requested, ownPaneID, logw, wait)
}

func waitForPanesUntilReady(ctx context.Context, rt runtimeapi.Runtime, requested []string, ownPaneID string, logw io.Writer, wait func(context.Context, time.Duration) error) ([]runtimeapi.Pane, error) {
	state := ""
	ownWarningLogged := false
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		allPanes, err := rt.ListPanes()
		if err != nil {
			if !retryablePaneListError(err) {
				return nil, err
			}
			if state != "error" && logw != nil {
				fmt.Fprintf(logw, "warning: waiting for panes: %v\n", err)
			}
			state = "error"
		} else {
			panes, excludedOwn := filterPanes(allPanes, requested, ownPaneID)
			if excludedOwn && !ownWarningLogged && logw != nil {
				fmt.Fprintf(logw, "warning: excluding own pane %s\n", ownPaneID)
				ownWarningLogged = true
			}
			if len(panes) > 0 {
				if state != "" && logw != nil {
					fmt.Fprintln(logw, "info: panes available")
				}
				return panes, nil
			}
			if state != "empty" && logw != nil {
				fmt.Fprintln(logw, "warning: waiting for panes: none of the requested panes were found")
			}
			state = "empty"
		}
		if err := wait(ctx, paneWaitBackoff); err != nil {
			return nil, err
		}
	}
}

func sleepForPanes(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryablePaneListError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection refused") ||
		strings.Contains(message, "no such file or directory") ||
		strings.Contains(message, "timed out") ||
		strings.Contains(message, "timeout") ||
		strings.Contains(message, "unexpected eof")
}

func runtimeForRun(cfg runConfig) (runtimeapi.Runtime, error) {
	if cfg.Runtime == "tmux" {
		return tmuxadapter.New()
	}
	if cfg.Transport == "socket" {
		if cfg.SocketExplicit && cfg.Socket == "" {
			return nil, errors.New("socket path is empty")
		}
		path, err := herdradapter.ResolveSocketPath(cfg.Socket)
		if err != nil {
			return nil, err
		}
		if err := herdradapter.ValidateSocketPath(path); err != nil {
			return nil, err
		}
		return herdradapter.NewSocket(herdradapter.SocketOptions{
			Path: path, Workspace: cfg.Workspace,
		}), nil
	}
	return herdradapter.New(herdradapter.Options{
		Bin:        cfg.HerdrBin,
		SocketPath: cfg.Socket,
		Session:    cfg.Session,
		Workspace:  cfg.Workspace,
	}), nil
}

func startDetectionPump(ctx context.Context, ticker <-chan time.Time, events <-chan runtimeapi.Event) <-chan time.Time {
	return startDetectionPumpWithInbox(ctx, ticker, events, nil)
}

func startDetectionPumpWithInbox(ctx context.Context, ticker <-chan time.Time, events <-chan runtimeapi.Event, inbox chan<- runtimeapi.Event) <-chan time.Time {
	ticks := make(chan time.Time, 8)
	go func() {
		defer close(ticks)
		var timer *time.Timer
		var timerC <-chan time.Time
		var pending bool
		var lastFire time.Time
		reset := func(delay time.Duration) {
			if timer == nil {
				timer = time.NewTimer(delay)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(delay)
			}
			timerC = timer.C
		}
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case tick, ok := <-ticker:
				if !ok {
					ticker = nil
					continue
				}
				select {
				case ticks <- tick:
				default:
				}
			case event, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				if inbox != nil {
					select {
					case inbox <- event:
					default:
					}
				}
				if event.Kind != runtimeapi.EventOutputMatched && event.Kind != runtimeapi.EventAgentStatus && event.Kind != runtimeapi.EventPaneMoved && event.Kind != runtimeapi.EventResync {
					continue
				}
				if event.Kind == runtimeapi.EventPaneMoved || event.Kind == runtimeapi.EventResync {
					select {
					case ticks <- time.Now():
					default:
					}
					continue
				}
				pending = true
				delay := 300 * time.Millisecond
				if !lastFire.IsZero() {
					since := time.Since(lastFire)
					if since < time.Second {
						if remaining := time.Second - since; remaining > delay {
							delay = remaining
						}
					}
				}
				reset(delay)
			case <-timerC:
				if !pending {
					timerC = nil
					continue
				}
				pending = false
				lastFire = time.Now()
				timerC = nil
				select {
				case ticks <- lastFire:
				default:
				}
			}
		}
	}()
	return ticks
}

func runCommand(args []string, _, stderr io.Writer) int {
	cfg, err := parseRunFlags(args, stderr)
	if err != nil {
		return 2
	}
	statePath := resolveStatePath(cfg)
	if statePath != "off" {
		runLock, lockErr := store.AcquireRunLock(statePath)
		if lockErr != nil {
			fmt.Fprintf(stderr, "Error: %v\n", lockErr)
			return 1
		}
		defer runLock.Release()
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	selfPaneID, err := rt.SelfPaneID()
	if err != nil {
		fmt.Fprintf(stderr, "Error: get own pane: %v\n", err)
		return 1
	}
	panes, err := listPanesStartup(ctx, rt, cfg.Panes, selfPaneID, cfg.WaitForPanes, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	monitoredTerminalIDs := make(map[string]struct{})
	for _, pane := range panes {
		if pane.TerminalID != "" {
			monitoredTerminalIDs[pane.TerminalID] = struct{}{}
		}
	}
	fmt.Fprintf(stderr, "state path: %s\n", statePath)
	coordOpts := make([]coordinator.Option, 0, 2)
	var manager *jobs.Manager
	if statePath != "off" {
		st := store.NewJSONStore(statePath)
		manager = jobs.New(rt, st, jobs.Config{
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
	coordOpts = append(coordOpts, coordinator.WithProviders(registry), coordinator.WithLogWriter(stderr))

	coord := coordinator.New(rt, coordinator.Config{
		OwnPaneID:   selfPaneID,
		TestPattern: cfg.TestPattern,
		DryRun:      cfg.DryRun,
		ReadLines:   cfg.Lines,
	}, coordOpts...)
	coord.SetPanes(panes)
	coord.Poll()
	coord.EnableAll()
	coord.Poll()

	detectionInterval := cfg.Interval
	if detectionInterval > 30*time.Second {
		detectionInterval = 30 * time.Second
	}
	detectionTicker := time.NewTicker(detectionInterval)
	defer detectionTicker.Stop()
	statusTicker := time.NewTicker(cfg.Interval)
	defer statusTicker.Stop()
	eventInbox := make(chan runtimeapi.Event, 64)
	var eventStream <-chan runtimeapi.Event
	var eventSource runtimeapi.EventSource
	if source, ok := rt.(runtimeapi.EventSource); ok {
		eventSource = source
		started, startErr := source.StartEvents(ctx, runtimeapi.SubscribeSpec{
			PaneIDs:    paneIDs(panes),
			MatchRegex: herdrDetectionMatchRegex,
			ReadSource: "detection",
			ReadLines:  cfg.Lines,
		})
		if startErr != nil {
			fmt.Fprintf(stderr, "warning: event stream unavailable; polling fallback: %v\n", startErr)
		} else {
			eventStream = started
		}
	}
	detectionTicks := startDetectionPumpWithInbox(ctx, detectionTicker.C, eventStream, eventInbox)
	var eventChannelOpen = true
	subscribedPaneIDs := paneIDs(panes)
	lastRecycle := time.Now()
	var lastTrigger time.Time
	coord.RunLoopWithCadence(ctx, detectionTicks, statusTicker.C, func() ([]runtimeapi.Pane, error) {
		var snapshot []runtimeapi.Pane
		var hasSnapshot bool
		if eventChannelOpen {
			for {
				select {
				case event, ok := <-eventInbox:
					if !ok {
						eventChannelOpen = false
						continue
					}
					if event.Kind == runtimeapi.EventResync {
						snapshot = append([]runtimeapi.Pane(nil), event.Snapshot...)
						hasSnapshot = true
						if manager != nil {
							if err := manager.ReconcilePanes(event.Snapshot); err != nil {
								fmt.Fprintf(stderr, "warning: reconcile pane identities: %v\n", err)
							}
						}
					} else if event.Kind == runtimeapi.EventPaneMoved && event.Pane.ID != "" {
						_, monitored := monitoredTerminalIDs[event.Pane.TerminalID]
						if !monitored {
							for _, requested := range cfg.Panes {
								if requested == event.PreviousPaneID {
									monitored = true
									break
								}
							}
						}
						if monitored {
							if manager != nil {
								if err := manager.ReassignPane(event.PreviousPaneID, event.Pane); err != nil {
									fmt.Fprintf(stderr, "warning: reassign pane identity: %v\n", err)
								}
							}
							for i, subscribed := range subscribedPaneIDs {
								if subscribed == event.PreviousPaneID {
									subscribedPaneIDs[i] = event.Pane.ID
								}
							}
							if event.Pane.TerminalID != "" {
								monitoredTerminalIDs[event.Pane.TerminalID] = struct{}{}
							}
							if eventSource != nil {
								eventSource.UpdateSubscribedPanes(append([]string(nil), subscribedPaneIDs...))
								lastRecycle = time.Now()
							}
						}
					} else if event.Kind == runtimeapi.EventOutputMatched || event.Kind == runtimeapi.EventAgentStatus {
						lastTrigger = time.Now()
					}
				default:
					if eventSource != nil && recycleDue(lastRecycle, lastTrigger, time.Now()) {
						eventSource.UpdateSubscribedPanes(append([]string(nil), subscribedPaneIDs...))
						lastRecycle = time.Now()
					}
					if hasSnapshot {
						filtered, _ := filterPanesByIdentity(snapshot, cfg.Panes, monitoredTerminalIDs, selfPaneID)
						return filtered, nil
					}
					goto refreshFromRuntime
				}
			}
		}
	refreshFromRuntime:
		all, err := rt.ListPanes()
		if err != nil {
			return nil, err
		}
		filtered, _ := filterPanesByIdentity(all, cfg.Panes, monitoredTerminalIDs, selfPaneID)
		return filtered, nil
	}, managerTick(manager), stderr)
	return 0
}

func recycleDue(lastRecycle, lastTrigger, now time.Time) bool {
	return !lastTrigger.IsZero() && lastTrigger.After(lastRecycle) && now.Sub(lastRecycle) >= time.Minute
}

func managerTick(manager *jobs.Manager) func(time.Time) {
	if manager == nil {
		return nil
	}
	return manager.Tick
}

func paneIDs(panes []runtimeapi.Pane) []string {
	ids := make([]string, 0, len(panes))
	for _, pane := range panes {
		ids = append(ids, pane.ID)
	}
	return ids
}
