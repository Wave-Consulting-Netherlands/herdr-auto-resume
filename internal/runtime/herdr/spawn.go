package herdr

import (
	"fmt"

	"github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/revive"
)

type reviveWorkspaceResult struct {
	WorkspaceID string      `json:"workspace_id"`
	PaneID      string      `json:"pane_id"`
	// RootPane is where herdr 0.7.5 actually reports the initial pane
	// (verified live: workspace_created carries root_pane.pane_id).
	RootPane  paneInfo    `json:"root_pane"`
	Workspace reviveSpace `json:"workspace"`
	Panes     []paneInfo  `json:"panes"`
}

type reviveSpace struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	PaneID      string     `json:"pane_id"`
	Panes       []paneInfo `json:"panes"`
}

// CreateWorkspace runs the exact operator workspace-create command and
// extracts the returned workspace and initial pane identifiers.
func (a *Adapter) CreateWorkspace(label, cwd string) (revive.Workspace, error) {
	output, err := a.execute("workspace", "create", "--label", label, "--cwd", cwd)
	if err != nil {
		return revive.Workspace{}, err
	}
	var result reviveWorkspaceResult
	if err := decodeResult(output, &result); err != nil {
		return revive.Workspace{}, err
	}
	workspaceID := result.WorkspaceID
	if workspaceID == "" {
		workspaceID = result.Workspace.WorkspaceID
	}
	if workspaceID == "" {
		workspaceID = result.Workspace.ID
	}
	paneID := result.PaneID
	if paneID == "" {
		paneID = result.RootPane.PaneID
	}
	if paneID == "" {
		paneID = result.Workspace.PaneID
	}
	panes := result.Panes
	if len(panes) == 0 {
		panes = result.Workspace.Panes
	}
	if paneID == "" && len(panes) > 0 {
		paneID = panes[0].PaneID
	}
	if workspaceID == "" || paneID == "" {
		return revive.Workspace{}, fmt.Errorf("workspace create returned incomplete workspace=%q pane=%q", workspaceID, paneID)
	}
	return revive.Workspace{WorkspaceID: workspaceID, PaneID: paneID}, nil
}

// RunPane starts Claude in the newly created pane. It intentionally accepts
// only the caller's explicit argv so revive sends no hidden continuation.
func (a *Adapter) RunPane(paneID string, args ...string) error {
	command := []string{"pane", "run", paneID}
	command = append(command, args...)
	_, err := a.execute(command...)
	return err
}

var _ revive.Spawner = (*Adapter)(nil)
