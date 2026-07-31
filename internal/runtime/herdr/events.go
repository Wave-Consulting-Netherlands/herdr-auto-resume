package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync/atomic"
	"time"

	runtimeapi "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

const (
	eventReadPoll   = 250 * time.Millisecond
	minEventBackoff = 500 * time.Millisecond
	maxEventBackoff = 15 * time.Second
)

type eventSession struct {
	socket   *Socket
	spec     runtimeapi.SubscribeSpec
	controls chan []string
	out      chan runtimeapi.Event
}

type eventFrame struct {
	Kind string          `json:"event"`
	Data json.RawMessage `json:"data"`
}

type eventData struct {
	PaneID              string     `json:"pane_id"`
	PreviousPaneID      string     `json:"previous_pane_id"`
	PreviousWorkspaceID string     `json:"previous_workspace_id"`
	AgentStatus         string     `json:"agent_status"`
	MatchedLine         string     `json:"matched_line"`
	Pane                paneInfo   `json:"pane"`
	Panes               []paneInfo `json:"panes"`
}

func (s *Socket) StartEvents(ctx context.Context, spec runtimeapi.SubscribeSpec) (<-chan runtimeapi.Event, error) {
	if ctx == nil {
		return nil, errors.New("event context is nil")
	}
	normalizeSubscribeSpec(&spec)
	conn, reader, err := s.connectEvents(ctx, spec)
	if err != nil {
		return nil, err
	}
	session := &eventSession{
		socket:   s,
		spec:     spec,
		controls: make(chan []string, 1),
		out:      make(chan runtimeapi.Event, 64),
	}
	s.eventsMu.Lock()
	if s.events != nil {
		s.eventsMu.Unlock()
		_ = conn.Close()
		return nil, errors.New("herdr events already started")
	}
	s.events = session
	s.eventsMu.Unlock()
	go session.run(ctx, conn, reader)
	return session.out, nil
}

func (s *Socket) UpdateSubscribedPanes(paneIDs []string) {
	s.eventsMu.Lock()
	session := s.events
	s.eventsMu.Unlock()
	if session == nil {
		return
	}
	ids := append([]string(nil), paneIDs...)
	select {
	case <-session.controls:
	default:
	}
	select {
	case session.controls <- ids:
	default:
	}
}

func normalizeSubscribeSpec(spec *runtimeapi.SubscribeSpec) {
	if spec.ReadSource == "" {
		spec.ReadSource = "detection"
	}
	if spec.ReadLines < 0 {
		spec.ReadLines = 0
	}
	spec.PaneIDs = append([]string(nil), spec.PaneIDs...)
}

func (s *Socket) connectEvents(ctx context.Context, spec runtimeapi.SubscribeSpec) (net.Conn, *bufio.Reader, error) {
	dialer := net.Dialer{Timeout: s.options.DialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", s.options.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("dial herdr events: %w", err)
	}
	if err := conn.SetDeadline(time.Now().Add(s.options.OpTimeout)); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("set herdr event deadline: %w", err)
	}
	reader := bufio.NewReader(conn)
	requestID, err := s.writeRequest(conn, "events.subscribe", subscriptionParams(spec))
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if err := conn.SetReadDeadline(time.Now().Add(s.options.OpTimeout)); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("set herdr event deadline: %w", err)
	}
	frame, err := readSocketFrame(reader)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("read herdr subscription response: %w", err)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(frame, &envelope); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("decode herdr subscription response: %w", err)
	}
	if !responseIDMatches(envelope.ID, requestID) {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("herdr subscription response id mismatch: got %s, want %d", string(envelope.ID), requestID)
	}
	if envelope.Error != nil {
		_ = conn.Close()
		return nil, nil, *envelope.Error
	}
	var result struct {
		Type string `json:"type"`
	}
	if len(envelope.Result) == 0 || json.Unmarshal(envelope.Result, &result) != nil || result.Type != "subscription_started" {
		_ = conn.Close()
		return nil, nil, errors.New("herdr subscription response missing subscription_started")
	}
	_ = conn.SetReadDeadline(time.Now().Add(eventReadPoll))
	return conn, reader, nil
}

func subscriptionParams(spec runtimeapi.SubscribeSpec) map[string]any {
	subscriptions := make([]map[string]any, 0, len(spec.PaneIDs)*2+5)
	for _, paneID := range spec.PaneIDs {
		subscriptions = append(subscriptions,
			map[string]any{"type": "pane.agent_status_changed", "pane_id": paneID},
			map[string]any{
				"type":       "pane.output_matched",
				"pane_id":    paneID,
				"source":     spec.ReadSource,
				"match":      map[string]any{"type": "regex", "value": spec.MatchRegex},
				"strip_ansi": true,
				"lines":      spec.ReadLines,
			},
		)
	}
	for _, kind := range []string{"pane.created", "pane.updated", "pane.closed", "pane.moved", "layout.updated"} {
		subscriptions = append(subscriptions, map[string]any{"type": kind})
	}
	return map[string]any{"subscriptions": subscriptions}
}

func (s *Socket) writeRequest(conn net.Conn, method string, params any) (uint64, error) {
	id := s.nextRequestID()
	request := struct {
		ID     uint64 `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{ID: id, Method: method, Params: params}
	payload, err := json.Marshal(request)
	if err != nil {
		return 0, fmt.Errorf("encode herdr request: %w", err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return 0, fmt.Errorf("write herdr request: %w", err)
	}
	return id, nil
}

func (s *Socket) nextRequestID() uint64 {
	return atomicAdd(&s.nextID)
}

func atomicAdd(value *uint64) uint64 {
	return atomic.AddUint64(value, 1)
}

func (session *eventSession) run(ctx context.Context, conn net.Conn, reader *bufio.Reader) {
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
		close(session.out)
		session.socket.eventsMu.Lock()
		if session.socket.events == session {
			session.socket.events = nil
		}
		session.socket.eventsMu.Unlock()
	}()
	pending := make(map[string]runtimeapi.Event)
	var pendingOrder []string
	var structural []runtimeapi.Event
	for {
		select {
		case <-ctx.Done():
			return
		case paneIDs := <-session.controls:
			session.spec.PaneIDs = append([]string(nil), paneIDs...)
			_ = conn.Close()
			var err error
			conn, reader, err = session.socket.connectEvents(ctx, session.spec)
			if err != nil {
				conn, reader = session.reconnect(ctx, pending, &pendingOrder, &structural)
				if conn == nil {
					return
				}
			}
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(eventReadPoll))
		frame, err := readSocketFrame(reader)
		if err != nil {
			flushEvents(session.out, pending, &pendingOrder, &structural)
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				continue
			}
			_ = conn.Close()
			conn, reader = session.reconnect(ctx, pending, &pendingOrder, &structural)
			if conn == nil {
				return
			}
			continue
		}
		var pushed eventFrame
		if json.Unmarshal(frame, &pushed) == nil {
			if event, ok := decodeEvent(pushed); ok {
				queueEvent(session.out, event, pending, &pendingOrder, &structural)
			}
		}
		flushEvents(session.out, pending, &pendingOrder, &structural)
	}
}

func (session *eventSession) reconnect(ctx context.Context, pending map[string]runtimeapi.Event, order *[]string, structural *[]runtimeapi.Event) (net.Conn, *bufio.Reader) {
	for delay := minEventBackoff; ; delay = nextBackoff(delay) {
		if !waitContext(ctx, jitter(delay)) {
			return nil, nil
		}
		if _, err := session.socket.Ping(); err != nil {
			continue
		}
		snapshot, err := session.socket.Snapshot()
		if err != nil {
			continue
		}
		conn, reader, err := session.socket.connectEvents(ctx, session.spec)
		if err != nil {
			continue
		}
		queueEvent(session.out, runtimeapi.Event{Kind: runtimeapi.EventResync, Snapshot: snapshot.Panes}, pending, order, structural)
		flushEvents(session.out, pending, order, structural)
		return conn, reader
	}
}

func nextBackoff(current time.Duration) time.Duration {
	if current >= maxEventBackoff {
		return maxEventBackoff
	}
	current *= 2
	if current > maxEventBackoff {
		return maxEventBackoff
	}
	return current
}

func jitter(value time.Duration) time.Duration {
	return time.Duration(float64(value) * (0.8 + rand.Float64()*0.4))
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func decodeEvent(frame eventFrame) (runtimeapi.Event, bool) {
	var data eventData
	if err := json.Unmarshal(frame.Data, &data); err != nil {
		return runtimeapi.Event{}, false
	}
	info := data.Pane
	if info.PaneID == "" {
		_ = json.Unmarshal(frame.Data, &info)
	}
	event := runtimeapi.Event{
		PaneID:              data.PaneID,
		PreviousPaneID:      data.PreviousPaneID,
		PreviousWorkspaceID: data.PreviousWorkspaceID,
		AgentStatus:         data.AgentStatus,
		MatchedLine:         data.MatchedLine,
		Pane:                paneFromInfo(info),
		Panes:               panesFromInfo(data.Panes),
	}
	if event.PaneID == "" {
		event.PaneID = event.Pane.ID
	}
	switch strings.ReplaceAll(frame.Kind, "_", ".") {
	case "pane.output.matched":
		event.Kind = runtimeapi.EventOutputMatched
	case "pane.agent.status.changed":
		event.Kind = runtimeapi.EventAgentStatus
	case "pane.moved":
		event.Kind = runtimeapi.EventPaneMoved
	case "pane.closed":
		event.Kind = runtimeapi.EventPaneClosed
	case "pane.created", "pane.updated", "pane.focused", "pane.exited", "pane.agent.detected", "layout.updated":
		event.Kind = runtimeapi.EventPanesChanged
	default:
		return runtimeapi.Event{}, false
	}
	return event, true
}

func queueEvent(out chan runtimeapi.Event, event runtimeapi.Event, pending map[string]runtimeapi.Event, order *[]string, structural *[]runtimeapi.Event) {
	select {
	case out <- event:
		return
	default:
	}
	if event.Kind == runtimeapi.EventPaneMoved || event.Kind == runtimeapi.EventResync {
		*structural = append(*structural, event)
		return
	}
	key := string(event.Kind) + ":" + event.PaneID
	if _, exists := pending[key]; !exists {
		*order = append(*order, key)
	}
	pending[key] = event
}

func flushEvents(out chan runtimeapi.Event, pending map[string]runtimeapi.Event, order *[]string, structural *[]runtimeapi.Event) {
	for len(*structural) > 0 {
		select {
		case out <- (*structural)[0]:
			*structural = (*structural)[1:]
		default:
			return
		}
	}
	for len(*order) > 0 {
		key := (*order)[0]
		event, ok := pending[key]
		if !ok {
			*order = (*order)[1:]
			continue
		}
		select {
		case out <- event:
			delete(pending, key)
			*order = (*order)[1:]
		default:
			return
		}
	}
}

var _ runtimeapi.EventSource = (*Socket)(nil)

func paneFromInfo(info paneInfo) runtimeapi.Pane {
	return runtimeapi.Pane{ID: info.PaneID, TerminalID: info.TerminalID, WorkspaceID: info.WorkspaceID, Title: info.TerminalTitle, Agent: info.Agent}
}
