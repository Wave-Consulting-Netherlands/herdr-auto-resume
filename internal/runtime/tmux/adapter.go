package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	runtimeapi "github.com/walt-verweij/herdr-auto-resume/internal/runtime"
)

// Adapter implements the runtime.Runtime interface using tmux commands.
type Adapter struct {
	pinnedWindowID string
}

// New validates the tmux environment and pins the current window once.
func New() (*Adapter, error) {
	if err := CheckTmuxEnv(); err != nil {
		return nil, err
	}

	windowID, err := CurrentWindowID()
	if err != nil {
		return nil, err
	}

	return &Adapter{pinnedWindowID: windowID}, nil
}

func (a *Adapter) Name() string {
	return "tmux"
}

func (a *Adapter) SelfPaneID() (string, error) {
	return CurrentPaneID()
}

func (a *Adapter) ListPanes() ([]runtimeapi.Pane, error) {
	layout, err := ListPanes(a.pinnedWindowID)
	if err != nil {
		return nil, err
	}

	panes := make([]runtimeapi.Pane, 0, len(layout.Panes))
	for _, pane := range layout.Panes {
		panes = append(panes, runtimeapi.Pane{
			ID:     pane.ID,
			Title:  pane.Title,
			Left:   pane.Left,
			Top:    pane.Top,
			Width:  pane.Width,
			Height: pane.Height,
		})
	}
	return panes, nil
}

func (a *Adapter) ReadPane(paneID string, lines int) (string, error) {
	// tmux capture-pane reads the visible viewport; lines is intentionally ignored
	// to preserve the upstream adapter behavior.
	return CapturePane(paneID)
}

func (a *Adapter) ProcessInfo(paneID string) (runtimeapi.ProcessInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", paneID, "#{pane_current_command}")
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return runtimeapi.ProcessInfo{}, ErrTimeout
		}
		return runtimeapi.ProcessInfo{}, fmt.Errorf("tmux display-message: %w", err)
	}

	return runtimeapi.ProcessInfo{Command: strings.TrimSpace(string(output))}, nil
}

func (a *Adapter) SendText(paneID, text string) error {
	return SendKeys(paneID, text)
}

func (a *Adapter) SendKeys(paneID string, keys ...string) error {
	return SendKeys(paneID, translateKeys(keys...)...)
}

func translateKeys(keys ...string) []string {
	translated := make([]string, len(keys))
	for i, key := range keys {
		switch key {
		case runtimeapi.KeyEscape:
			translated[i] = "Escape"
		case runtimeapi.KeyEnter:
			translated[i] = "Enter"
		default:
			translated[i] = key
		}
	}
	return translated
}

func (a *Adapter) Notify(title, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	// Use an argument array so notification text is never interpreted by a shell.
	args := []string{"display-message", fmt.Sprintf("%s: %s", title, body)}
	cmd := exec.CommandContext(ctx, "tmux", args...)
	_ = cmd.Run()
	return nil
}

var _ runtimeapi.Runtime = (*Adapter)(nil)
