package runtime

// Pane describes a runtime pane's identity, title, geometry, and agent.
type Pane struct {
	ID, TerminalID, WorkspaceID, Title string
	Left, Top, Width, Height           int
	Agent                              string
	AgentSessionID                     string
}

// ProcessInfo describes the foreground process associated with a pane.
type ProcessInfo struct {
	Command, CWD string
}

// Runtime is the minimum control surface required by the resume coordinator.
type Runtime interface {
	Name() string
	SelfPaneID() (string, error)
	ListPanes() ([]Pane, error)
	ReadPane(paneID string, lines int) (string, error)
	ProcessInfo(paneID string) (ProcessInfo, error)
	SendText(paneID, text string) error
	SendKeys(paneID string, keys ...string) error
	Notify(title, body string) error
}
