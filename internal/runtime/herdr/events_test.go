package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

type streamHarness struct {
	listener *net.UnixListener
	accepted chan *net.UnixConn
}

func newStreamHarness(t *testing.T) *streamHarness {
	t.Helper()
	path := t.TempDir() + "/herdr.sock"
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	h := &streamHarness{listener: listener, accepted: make(chan *net.UnixConn, 16)}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			h.accepted <- conn
		}
	}()
	return h
}

func (h *streamHarness) path() string { return h.listener.Addr().String() }

func (h *streamHarness) next(t *testing.T) *net.UnixConn {
	t.Helper()
	select {
	case conn := <-h.accepted:
		return conn
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for socket connection")
		return nil
	}
}

func readStreamRequest(t *testing.T, conn *net.UnixConn) socketRequest {
	t.Helper()
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var request socketRequest
	if err := json.Unmarshal(line, &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func writeStreamResponse(t *testing.T, conn *net.UnixConn, id json.RawMessage, result any) {
	t.Helper()
	payload, err := json.Marshal(socketResponse(id, result))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		t.Fatal(err)
	}
}

func writeStreamEvent(t *testing.T, conn *net.UnixConn, kind string, data any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"event": kind, "data": data})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		t.Fatal(err)
	}
}

func startStream(t *testing.T, h *streamHarness, spec runtimeapi.SubscribeSpec) (*Socket, <-chan runtimeapi.Event, *net.UnixConn, socketRequest, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct {
		ch  <-chan runtimeapi.Event
		err error
	}, 1)
	client := NewSocket(SocketOptions{Path: h.path(), DialTimeout: time.Second, OpTimeout: time.Second})
	go func() {
		ch, err := client.StartEvents(ctx, spec)
		started <- struct {
			ch  <-chan runtimeapi.Event
			err error
		}{ch: ch, err: err}
	}()
	conn := h.next(t)
	request := readStreamRequest(t, conn)
	writeStreamResponse(t, conn, request.ID, map[string]any{"type": "subscription_started"})
	result := <-started
	if result.err != nil {
		t.Fatalf("StartEvents(): %v", result.err)
	}
	t.Cleanup(func() { cancel(); _ = conn.Close() })
	return client, result.ch, conn, request, cancel
}

func TestSocketEventsSubscribeAndDecodeBothEventVocabularies(t *testing.T) {
	h := newStreamHarness(t)
	_, ch, conn, request, _ := startStream(t, h, runtimeapi.SubscribeSpec{
		PaneIDs: []string{"p1", "p2"}, MatchRegex: "(?i)limit", ReadSource: "detection", ReadLines: 42,
	})
	if request.Method != "events.subscribe" {
		t.Fatalf("method = %q", request.Method)
	}
	data, err := json.Marshal(request.Params)
	if err != nil {
		t.Fatal(err)
	}
	var params struct {
		Subscriptions []map[string]any `json:"subscriptions"`
	}
	if err := json.Unmarshal(data, &params); err != nil {
		t.Fatal(err)
	}
	if len(params.Subscriptions) != 9 {
		t.Fatalf("subscriptions = %#v, want two pane pairs plus lifecycle/layout", params.Subscriptions)
	}
	if strings.Contains(string(data), `"type":"pane.agent_detected"`) {
		t.Fatalf("default subscriptions = %s, want agent detection opt-in", data)
	}
	if params.Subscriptions[0]["type"] != "pane.agent_status_changed" || params.Subscriptions[0]["pane_id"] != "p1" {
		t.Fatalf("first subscription = %#v", params.Subscriptions[0])
	}
	if params.Subscriptions[1]["type"] != "pane.output_matched" || params.Subscriptions[1]["source"] != "detection" || params.Subscriptions[1]["lines"] != float64(42) {
		t.Fatalf("output subscription = %#v", params.Subscriptions[1])
	}

	writeStreamEvent(t, conn, "pane.output_matched", map[string]any{"pane_id": "p1", "matched_line": "limit"})
	writeStreamEvent(t, conn, "pane_agent_status_changed", map[string]any{"pane_id": "p1", "agent_status": "blocked"})
	writeStreamEvent(t, conn, "pane_moved", map[string]any{"previous_pane_id": "p1", "previous_workspace_id": "w1", "pane": map[string]any{"pane_id": "p9", "terminal_id": "term-1", "agent": "claude"}})
	writeStreamEvent(t, conn, "pane_created", map[string]any{"pane": map[string]any{"pane_id": "p2", "terminal_id": "term-2"}})
	writeStreamEvent(t, conn, "layout.updated", map[string]any{})
	writeStreamEvent(t, conn, "unknown.event", map[string]any{})

	got := make([]runtimeapi.Event, 0, 5)
	deadline := time.After(2 * time.Second)
	for len(got) < 5 {
		select {
		case event, ok := <-ch:
			if !ok {
				t.Fatal("event channel closed early")
			}
			got = append(got, event)
		case <-deadline:
			t.Fatalf("events = %#v", got)
		}
	}
	if got[0].Kind != runtimeapi.EventOutputMatched || got[0].PaneID != "p1" || got[0].MatchedLine != "limit" {
		t.Fatalf("output event = %#v", got[0])
	}
	if got[1].Kind != runtimeapi.EventAgentStatus || got[1].AgentStatus != "blocked" {
		t.Fatalf("status event = %#v", got[1])
	}
	if got[2].Kind != runtimeapi.EventPaneMoved || got[2].PreviousPaneID != "p1" || got[2].Pane.ID != "p9" {
		t.Fatalf("move event = %#v", got[2])
	}
	if got[3].Kind != runtimeapi.EventPanesChanged || got[4].Kind != runtimeapi.EventPanesChanged {
		t.Fatalf("lifecycle events = %#v", got[3:])
	}
}

func TestSocketEventsSubscribesAndDecodesAgentDetected(t *testing.T) {
	h := newStreamHarness(t)
	_, ch, conn, request, _ := startStream(t, h, runtimeapi.SubscribeSpec{PaneIDs: []string{"p1"}, MatchRegex: "limit", AdmitAgentEvents: true})
	data, err := json.Marshal(request.Params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"type":"pane.agent_detected"`) {
		t.Fatalf("subscriptions = %s, want pane.agent_detected", data)
	}
	writeStreamEvent(t, conn, "pane.agent_detected", map[string]any{
		"pane": map[string]any{"pane_id": "p2", "agent": "claude", "terminal_id": "term-2"},
	})
	select {
	case event := <-ch:
		if event.Kind != runtimeapi.EventKind("agent_detected") || event.Pane.ID != "p2" || event.Pane.Agent != "claude" {
			t.Fatalf("agent event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for agent-detected event")
	}
}

func TestSocketEventsOversizedFrameAndSlowConsumerCoalesceTriggers(t *testing.T) {
	h := newStreamHarness(t)
	_, ch, conn, _, _ := startStream(t, h, runtimeapi.SubscribeSpec{PaneIDs: []string{"p1"}, MatchRegex: "limit"})
	large := strings.Repeat("x", 128*1024)
	for i := 0; i < 300; i++ {
		writeStreamEvent(t, conn, "pane.output_matched", map[string]any{"pane_id": "p1", "matched_line": large + string(rune('a'+i%26))})
	}
	writeStreamEvent(t, conn, "pane_moved", map[string]any{"previous_pane_id": "p1", "pane": map[string]any{"pane_id": "p2", "terminal_id": "term-1"}})
	var sawLarge, sawMove bool
	deadline := time.After(3 * time.Second)
	for !sawMove {
		select {
		case event := <-ch:
			if event.Kind == runtimeapi.EventOutputMatched && len(event.MatchedLine) > 64*1024 {
				sawLarge = true
			}
			if event.Kind == runtimeapi.EventPaneMoved {
				sawMove = true
			}
		case <-deadline:
			t.Fatal("slow consumer did not receive retained pane_moved event")
		}
	}
	if !sawLarge {
		t.Fatal("oversized event frame was not decoded")
	}
}

func TestSocketEventsReconnectsWithPingSnapshotResubscribeAndResync(t *testing.T) {
	h := newStreamHarness(t)
	_, ch, conn, _, cancel := startStream(t, h, runtimeapi.SubscribeSpec{PaneIDs: []string{"p1"}, MatchRegex: "limit"})
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	go func() {
		pingConn := h.next(t)
		ping := readStreamRequest(t, pingConn)
		writeStreamResponse(t, pingConn, ping.ID, map[string]any{"type": "pong", "version": "0.7.5", "protocol": 17})
		_ = pingConn.Close()

		snapshotConn := h.next(t)
		snapshot := readStreamRequest(t, snapshotConn)
		writeStreamResponse(t, snapshotConn, snapshot.ID, map[string]any{"type": "snapshot", "snapshot": map[string]any{"panes": []any{map[string]any{"pane_id": "p2", "terminal_id": "term-1"}}}})
		_ = snapshotConn.Close()

		subConn := h.next(t)
		sub := readStreamRequest(t, subConn)
		writeStreamResponse(t, subConn, sub.ID, map[string]any{"type": "subscription_started"})
	}()
	select {
	case event := <-ch:
		if event.Kind != runtimeapi.EventResync || !reflect.DeepEqual(event.Snapshot, []runtimeapi.Pane{{ID: "p2", TerminalID: "term-1"}}) {
			t.Fatalf("resync event = %#v", event)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for reconnect resync")
	}
	cancel()
}

func TestSocketEventsUpdateSubscribedPanesRecyclesConnection(t *testing.T) {
	h := newStreamHarness(t)
	client, ch, _, _, cancel := startStream(t, h, runtimeapi.SubscribeSpec{PaneIDs: []string{"p1"}, MatchRegex: "limit"})
	client.UpdateSubscribedPanes([]string{"p2"})
	recycled := h.next(t)
	request := readStreamRequest(t, recycled)
	if request.Method != "events.subscribe" {
		t.Fatalf("recycle method = %q", request.Method)
	}
	data, _ := json.Marshal(request.Params)
	if !strings.Contains(string(data), `"pane_id":"p2"`) || strings.Contains(string(data), `"pane_id":"p1"`) {
		t.Fatalf("recycled subscriptions = %s", data)
	}
	writeStreamResponse(t, recycled, request.ID, map[string]any{"type": "subscription_started"})
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("event channel remained open after context cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("event goroutine did not join after context cancellation")
	}
}
