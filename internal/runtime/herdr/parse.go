package herdr

import (
	"encoding/json"
	"fmt"
)

type HerdrError struct {
	Code    string
	Message string
}

func (e HerdrError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	if e.Message == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type responseEnvelope struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *HerdrError     `json:"error"`
}

func decodeResult(data []byte, target any) error {
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode herdr response: %w", err)
	}
	if envelope.Error != nil {
		return *envelope.Error
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return fmt.Errorf("decode herdr response: missing result")
	}
	if err := json.Unmarshal(envelope.Result, target); err != nil {
		return fmt.Errorf("decode herdr result: %w", err)
	}
	return nil
}

func decodeError(data []byte) (HerdrError, bool) {
	var envelope responseEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Error == nil {
		return HerdrError{}, false
	}
	return *envelope.Error, true
}

type paneListResult struct {
	Panes []paneInfo `json:"panes"`
}

type paneInfo struct {
	PaneID        string `json:"pane_id"`
	TerminalID    string `json:"terminal_id"`
	WorkspaceID   string `json:"workspace_id"`
	TabID         string `json:"tab_id"`
	Agent         string `json:"agent"`
	AgentStatus   string `json:"agent_status"`
	Revision      int64  `json:"revision"`
	CWD           string `json:"cwd"`
	TerminalTitle string `json:"terminal_title"`
}

type pongResult struct {
	Type     string `json:"type"`
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

type snapshotResult struct {
	Type     string `json:"type"`
	Protocol int    `json:"protocol"`
	Snapshot struct {
		Panes []paneInfo `json:"panes"`
	} `json:"snapshot"`
	Panes []paneInfo `json:"panes"`
}

type paneReadResult struct {
	Read struct {
		Text      string `json:"text"`
		Revision  int64  `json:"revision"`
		Truncated bool   `json:"truncated"`
	} `json:"read"`
}

type processInfoResult struct {
	ProcessInfo struct {
		ForegroundProcesses []struct {
			Name    string `json:"name"`
			Cmdline string `json:"cmdline"`
			CWD     string `json:"cwd"`
		} `json:"foreground_processes"`
	} `json:"process_info"`
}
