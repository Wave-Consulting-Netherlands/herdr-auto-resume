package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
	herdradapter "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime/herdr"
	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/store"
)

type doctorConfig struct {
	Transport string
	Bin       string
	Socket    string
	Session   string
	Workspace string
	StateFile string
}

type doctorDeps struct {
	resolve    func(string) (string, error)
	run        func(string, ...string) ([]byte, error)
	socket     func(string) error
	home       func() (string, error)
	newAdapter func(herdradapter.Options, herdradapter.ExecFunc) runtime.Runtime
	newSocket  func(herdradapter.SocketOptions) *herdradapter.Socket
}

func defaultDoctorDeps() doctorDeps {
	return doctorDeps{
		resolve: exec.LookPath,
		run: func(bin string, args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, args...)
			output, err := cmd.Output()
			if ctx.Err() == context.DeadlineExceeded {
				return output, errors.New("command timed out")
			}
			return output, err
		},
		socket: func(path string) error {
			_, err := os.Stat(path)
			return err
		},
		home: os.UserHomeDir,
		newAdapter: func(o herdradapter.Options, run herdradapter.ExecFunc) runtime.Runtime {
			return herdradapter.NewWithExec(o, run)
		},
		newSocket: func(o herdradapter.SocketOptions) *herdradapter.Socket {
			return herdradapter.NewSocket(o)
		},
	}
}

func parseDoctorFlags(args []string, stderr io.Writer) (doctorConfig, error) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: herdr-auto-resume doctor [options]")
		fs.PrintDefaults()
	}
	var cfg doctorConfig
	fs.StringVar(&cfg.Transport, "transport", "cli", "herdr transport: cli or socket")
	fs.StringVar(&cfg.Bin, "herdr-bin", "herdr", "herdr binary path")
	fs.StringVar(&cfg.Socket, "socket", "", "herdr socket path")
	fs.StringVar(&cfg.Session, "session", "", "herdr session")
	fs.StringVar(&cfg.Workspace, "workspace", "", "herdr workspace")
	fs.StringVar(&cfg.StateFile, "state-file", "auto", "watcher state file path, auto, or off")
	if err := fs.Parse(args); err != nil {
		fs.Usage()
		return doctorConfig{}, err
	}
	if cfg.Transport != "cli" && cfg.Transport != "socket" {
		err := fmt.Errorf("unsupported transport %q", cfg.Transport)
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return doctorConfig{}, err
	}
	if cfg.Transport == "socket" && cfg.Session != "" {
		err := errors.New("--session is unsupported with socket transport; use --socket")
		fmt.Fprintln(stderr, "error:", err)
		fs.Usage()
		return doctorConfig{}, err
	}
	return cfg, nil
}

func doctorLine(out io.Writer, status, check, detail string) {
	fmt.Fprintf(out, "%s %s: %s\n", status, check, detail)
}

func parseHerdrVersion(output []byte) (string, bool) {
	fields := strings.Fields(string(output))
	if len(fields) < 2 || fields[0] != "herdr" || fields[1] == "" {
		return "", false
	}
	return strings.Join(fields, " "), true
}

func protocolDetail(output []byte) (string, bool) {
	var payload any
	decoder := json.NewDecoder(strings.NewReader(string(output)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return "", false
	}
	if protocol, ok := findProtocol(payload); ok {
		return fmt.Sprintf("protocol %d", protocol), true
	}
	return "", false
}

func findProtocol(value any) (int64, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if strings.EqualFold(key, "protocol") || strings.EqualFold(key, "protocol_version") {
				if number, ok := protocolNumber(nested); ok {
					return number, true
				}
			}
			if number, ok := findProtocol(nested); ok {
				return number, true
			}
		}
	case []any:
		for _, nested := range typed {
			if number, ok := findProtocol(nested); ok {
				return number, true
			}
		}
	}
	return 0, false
}

func protocolNumber(value any) (int64, bool) {
	var text string
	switch typed := value.(type) {
	case json.Number:
		text = typed.String()
	case string:
		text = strings.TrimSpace(typed)
	default:
		return 0, false
	}
	number, err := strconv.ParseInt(text, 10, 64)
	return number, err == nil && number > 0
}

func runDoctorCommand(args []string, out io.Writer, deps doctorDeps) int {
	currentVersion, currentCommit, _ := versionInfo()
	doctorLine(out, "INFO", "version", fmt.Sprintf("herdr-auto-resume %s (%s)", currentVersion, currentCommit))
	cfg, err := parseDoctorFlags(args, out)
	if err != nil {
		return 2
	}
	watcherOK := reportWatcherLock(out, resolveStatePath(runConfig{Runtime: "herdr", StateFile: cfg.StateFile}))
	if cfg.Transport == "socket" {
		if !watcherOK {
			return 1
		}
		return runSocketDoctor(cfg, out, deps)
	}
	failed := !watcherOK
	resolvedBin, err := deps.resolve(cfg.Bin)
	if err != nil {
		doctorLine(out, "FAIL", "binary", fmt.Sprintf("%s is not resolvable: %v", cfg.Bin, err))
		failed = true
		resolvedBin = cfg.Bin
	} else {
		versionOutput, versionErr := deps.run(resolvedBin, "--version")
		if versionErr != nil {
			doctorLine(out, "FAIL", "binary", fmt.Sprintf("%s --version failed: %v", resolvedBin, versionErr))
			failed = true
		} else if version, ok := parseHerdrVersion(versionOutput); !ok {
			doctorLine(out, "FAIL", "binary", fmt.Sprintf("unparseable version output %q", strings.TrimSpace(string(versionOutput))))
			failed = true
		} else {
			doctorLine(out, "PASS", "binary", fmt.Sprintf("%s (%s)", resolvedBin, version))
		}
	}

	socketPath := cfg.Socket
	if socketPath == "" {
		home, homeErr := deps.home()
		if homeErr != nil {
			doctorLine(out, "FAIL", "socket", fmt.Sprintf("find home directory: %v", homeErr))
			failed = true
			socketPath = ""
		} else {
			socketPath = home + "/.config/herdr/herdr.sock"
		}
	}
	if socketPath != "" {
		if socketErr := deps.socket(socketPath); socketErr != nil {
			doctorLine(out, "FAIL", "socket", fmt.Sprintf("%s: %v", socketPath, socketErr))
			failed = true
		} else {
			doctorLine(out, "PASS", "socket", socketPath)
		}
	}

	statusOutput, statusErr := deps.run(resolvedBin, "status")
	if statusErr != nil {
		statusOutput, statusErr = deps.run(resolvedBin, "api", "snapshot")
	}
	if statusErr != nil {
		doctorLine(out, "FAIL", "status", fmt.Sprintf("status and api snapshot failed: %v", statusErr))
		failed = true
	} else {
		if detail, ok := protocolDetail(statusOutput); ok {
			doctorLine(out, "PASS", "status", detail)
		} else {
			doctorLine(out, "WARN", "status", "protocol unknown")
		}
	}

	adapter := deps.newAdapter(herdradapter.Options{
		Bin:        resolvedBin,
		SocketPath: socketPath,
		Session:    cfg.Session,
		Workspace:  cfg.Workspace,
	}, func(args ...string) ([]byte, error) {
		return deps.run(resolvedBin, args...)
	})
	panes, adapterErr := adapter.ListPanes()
	if adapterErr != nil {
		doctorLine(out, "FAIL", "adapter", fmt.Sprintf("list panes: %v", adapterErr))
		failed = true
	} else {
		doctorLine(out, "PASS", "adapter", fmt.Sprintf("decoded %d panes", len(panes)))
	}

	schemaOutput, schemaErr := deps.run(resolvedBin, "api", "schema", "--json")
	if schemaErr != nil || !json.Valid(schemaOutput) {
		detail := "unparseable JSON (known herdr 0.7.5 control-character quirk)"
		if schemaErr != nil {
			detail = schemaErr.Error()
		}
		doctorLine(out, "WARN", "schema", detail)
	} else {
		doctorLine(out, "PASS", "schema", "valid JSON")
	}

	if paneID := os.Getenv("HERDR_PANE_ID"); paneID != "" {
		doctorLine(out, "PASS", "self", fmt.Sprintf("running inside %s; self-exclusion applies", paneID))
	} else {
		doctorLine(out, "WARN", "self", "HERDR_PANE_ID is unset; not running inside a herdr pane")
	}
	if failed {
		return 1
	}
	return 0
}

func reportWatcherLock(out io.Writer, statePath string) bool {
	if statePath == "off" {
		doctorLine(out, "INFO", "watcher", "none on off")
		return true
	}
	lock, err := store.AcquireRunLock(statePath)
	if err == nil {
		_ = lock.Release()
		doctorLine(out, "INFO", "watcher", fmt.Sprintf("none on %s", statePath))
		return true
	}
	var held *store.RunLockHeldError
	if errors.As(err, &held) {
		doctorLine(out, "INFO", "watcher", fmt.Sprintf("active (pid %s) on %s", held.PID, held.StatePath))
		return true
	}
	doctorLine(out, "FAIL", "watcher", err.Error())
	return false
}

func runSocketDoctor(cfg doctorConfig, out io.Writer, deps doctorDeps) int {
	socketPath := cfg.Socket
	if socketPath == "" {
		home, err := deps.home()
		if err != nil {
			doctorLine(out, "FAIL", "socket", fmt.Sprintf("find home directory: %v", err))
			return 1
		}
		socketPath = home + "/.config/herdr/herdr.sock"
	}
	newSocket := deps.newSocket
	if newSocket == nil {
		newSocket = func(o herdradapter.SocketOptions) *herdradapter.Socket { return herdradapter.NewSocket(o) }
	}
	client := newSocket(herdradapter.SocketOptions{Path: socketPath})
	pong, err := client.Ping()
	if err != nil {
		doctorLine(out, "FAIL", "ping", err.Error())
		return 1
	}
	if pong.Protocol == 17 {
		doctorLine(out, "PASS", "ping", fmt.Sprintf("protocol %d", pong.Protocol))
	} else {
		doctorLine(out, "WARN", "ping", fmt.Sprintf("protocol %d (expected 17)", pong.Protocol))
	}

	snapshot, err := client.Snapshot()
	if err != nil {
		doctorLine(out, "FAIL", "snapshot", err.Error())
		return 1
	}
	if snapshot.Protocol != 0 && pong.Protocol != 0 && snapshot.Protocol != pong.Protocol {
		doctorLine(out, "FAIL", "snapshot", fmt.Sprintf("protocol %d disagrees with ping protocol %d", snapshot.Protocol, pong.Protocol))
		return 1
	}
	detail := fmt.Sprintf("decoded %d panes", len(snapshot.Panes))
	if snapshot.Protocol != 0 {
		detail += fmt.Sprintf("; protocol %d", snapshot.Protocol)
	}
	doctorLine(out, "PASS", "snapshot", detail)
	if err := client.SubscribeLayout(context.Background()); err != nil {
		doctorLine(out, "FAIL", "events", err.Error())
		return 1
	}
	doctorLine(out, "PASS", "events", "layout.updated subscription_started")
	doctorLine(out, "PASS", "socket", socketPath)
	return 0
}

func doctorCommand(args []string, stdout, stderr io.Writer) int {
	return runDoctorCommand(args, stdout, defaultDoctorDeps())
}
