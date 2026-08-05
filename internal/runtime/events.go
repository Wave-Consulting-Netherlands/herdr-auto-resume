package runtime

import "context"

type EventKind string

const (
	EventOutputMatched EventKind = "output_matched"
	EventAgentStatus   EventKind = "agent_status"
	EventPaneMoved     EventKind = "pane_moved"
	EventPaneClosed    EventKind = "pane_closed"
	EventPanesChanged  EventKind = "panes_changed"
	EventAgentDetected EventKind = "agent_detected"
	EventResync        EventKind = "resync"
)

// Event is the neutral, coordinator-facing event model. Adapters decode their
// native event envelopes into this type; the coordinator never imports them.
type Event struct {
	Kind                EventKind
	PaneID              string
	PreviousPaneID      string
	PreviousWorkspaceID string
	AgentStatus         string
	MatchedLine         string
	Pane                Pane
	Panes               []Pane
	Snapshot            []Pane
}

type SubscribeSpec struct {
	PaneIDs          []string
	MatchRegex       string
	ReadSource       string
	ReadLines        int
	AdmitAgentEvents bool
}

// EventSource is an optional Runtime capability. Runtime itself intentionally
// remains unchanged so tmux, CLI, and fake adapters need no event stubs.
type EventSource interface {
	StartEvents(context.Context, SubscribeSpec) (<-chan Event, error)
	UpdateSubscribedPanes([]string)
}
