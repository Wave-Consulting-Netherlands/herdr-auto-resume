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

	runtimeapi "github.com/walt-verweij/herdr-auto-resume/internal/runtime"
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

type runFunc func(args ...string) (stdout []byte, exitErr error)

type Adapter struct {
	options Options
	run     runFunc
}

func New(o Options) *Adapter {
	if o.Bin == "" {
		o.Bin = "herdr"
	}
	if o.ReadSource == "" {
		o.ReadSource = "recent"
	}
	return &Adapter{options: o, run: productionRunner(o)}
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
		panes = append(panes, runtimeapi.Pane{ID: pane.PaneID, Title: pane.TerminalTitle, Agent: pane.Agent})
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
		switch key {
		case runtimeapi.KeyEscape:
			args = append(args, "esc")
		case runtimeapi.KeyEnter:
			args = append(args, "enter")
		default:
			args = append(args, key)
		}
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
