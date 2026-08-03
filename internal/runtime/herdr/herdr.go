package herdr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	runtimeapi "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

const commandTimeout = 5 * time.Second

var ErrTimeout = errors.New("herdr command timed out")

type Options struct {
	Bin        string
	SocketPath string
	Session    string
	Workspace  string
	ReadSource string
}

type ExecFunc func(args ...string) (stdout []byte, exitErr error)

type runFunc = ExecFunc

type Adapter struct {
	options Options
	run     runFunc
}

func New(o Options) *Adapter {
	return NewWithExec(o, productionRunner(o))
}

// NewWithExec constructs an adapter with an injected command runner. It is
// useful for callers that need to test protocol handling without invoking herdr.
func NewWithExec(o Options, run ExecFunc) *Adapter {
	if o.Bin == "" {
		o.Bin = "herdr"
	}
	if o.ReadSource == "" {
		// "detection" includes the visible viewport; "recent" covers only
		// scrollback and is empty on fresh/quiet panes, which would blind
		// limit detection exactly when the banner is still on screen.
		o.ReadSource = "detection"
	}
	if run == nil {
		run = productionRunner(o)
	}
	return &Adapter{options: o, run: run}
}

func productionRunner(o Options) runFunc {
	return func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, o.Bin, args...)
		cmd.Env = currentChildEnvironment(o.SocketPath)
		output, err := cmd.Output()
		if err == nil {
			return output, nil
		}
		if ctx.Err() == context.DeadlineExceeded {
			return output, ErrTimeout
		}
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return output, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return output, err
	}
}

func (a *Adapter) commandArgs(args ...string) []string {
	result := append([]string(nil), args...)
	if a.options.Session != "" {
		result = append(result, "--session", a.options.Session)
	}
	return result
}

func (a *Adapter) execute(args ...string) ([]byte, error) {
	output, exitErr := a.run(a.commandArgs(args...)...)
	if exitErr == nil {
		return output, nil
	}
	if herdrErr, ok := decodeError(output); ok {
		return nil, herdrErr
	}
	return nil, fmt.Errorf("herdr command failed: %w", exitErr)
}

func (a *Adapter) Name() string {
	return "herdr"
}

func (a *Adapter) SelfPaneID() (string, error) {
	return os.Getenv("HERDR_PANE_ID"), nil
}

func (a *Adapter) ListPanes() ([]runtimeapi.Pane, error) {
	args := []string{"pane", "list"}
	if a.options.Workspace != "" {
		args = append(args, "--workspace", a.options.Workspace)
	}
	output, err := a.execute(args...)
	if err != nil {
		return nil, err
	}
	var result paneListResult
	if err := decodeResult(output, &result); err != nil {
		return nil, err
	}
	panes := make([]runtimeapi.Pane, 0, len(result.Panes))
	for _, pane := range result.Panes {
		panes = append(panes, runtimeapi.Pane{ID: pane.PaneID, TerminalID: pane.TerminalID, WorkspaceID: pane.WorkspaceID, Title: pane.TerminalTitle, CWD: pane.CWD, Agent: pane.Agent, AgentSessionID: pane.AgentSession.Value})
	}
	return panes, nil
}

func (a *Adapter) ReadPane(paneID string, lines int) (string, error) {
	args := []string{"pane", "read", paneID, "--source", a.options.ReadSource}
	if lines > 0 {
		args = append(args, "--lines", strconv.Itoa(lines))
	}
	output, err := a.execute(args...)
	return string(output), err
}

func (a *Adapter) ProcessInfo(paneID string) (runtimeapi.ProcessInfo, error) {
	output, err := a.execute("pane", "process-info", "--pane", paneID)
	if err != nil {
		return runtimeapi.ProcessInfo{}, err
	}
	var result processInfoResult
	if err := decodeResult(output, &result); err != nil {
		return runtimeapi.ProcessInfo{}, err
	}
	if len(result.ProcessInfo.ForegroundProcesses) == 0 {
		return runtimeapi.ProcessInfo{}, nil
	}
	process := result.ProcessInfo.ForegroundProcesses[0]
	command := process.Name
	if command == "" {
		command = process.Cmdline
	}
	return runtimeapi.ProcessInfo{Command: command, CWD: process.CWD}, nil
}

func (a *Adapter) SendText(paneID, text string) error {
	_, err := a.execute("pane", "send-text", paneID, text)
	return err
}

func (a *Adapter) SendKeys(paneID string, keys ...string) error {
	args := []string{"pane", "send-keys", paneID}
	for _, key := range keys {
		args = append(args, herdrKey(key))
	}
	_, err := a.execute(args...)
	return err
}

func (a *Adapter) Notify(title, body string) error {
	args := []string{"notification", "show", title}
	if body != "" {
		args = append(args, "--body", body)
	}
	_, err := a.execute(args...)
	return err
}

var _ runtimeapi.Runtime = (*Adapter)(nil)
