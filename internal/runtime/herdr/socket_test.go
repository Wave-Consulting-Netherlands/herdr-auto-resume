package herdr

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	runtimeapi "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

type socketHarness struct {
	t        *testing.T
	listener *net.UnixListener
	handler  func(socketRequest) any
	mu       sync.Mutex
	requests []socketRequest
}

type socketRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params map[string]any  `json:"params"`
}

func newSocketHarness(t *testing.T, handler func(socketRequest) any) *socketHarness {
	t.Helper()
	path := t.TempDir() + "/herdr.sock"
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	h := &socketHarness{t: t, listener: listener, handler: handler}
	t.Cleanup(func() { _ = listener.Close() })
	go h.serve()
	return h
}

func (h *socketHarness) path() string { return h.listener.Addr().String() }

func (h *socketHarness) serve() {
	for {
		conn, err := h.listener.AcceptUnix()
		if err != nil {
			return
		}
		go h.serveConn(conn)
	}
}

func (h *socketHarness) serveConn(conn *net.UnixConn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}
	var request socketRequest
	if err := json.Unmarshal(line, &request); err != nil {
		h.t.Errorf("decode request: %v", err)
		return
	}
	h.mu.Lock()
	h.requests = append(h.requests, request)
	h.mu.Unlock()
	response := h.handler(request)
	if response == nil {
		return
	}
	payload, err := json.Marshal(response)
	if err != nil {
		h.t.Errorf("encode response: %v", err)
		return
	}
	_, _ = conn.Write(append(payload, '\n'))
	// The real server resets a connection that receives a second request. A
	// short non-blocking check makes that rule part of this harness too.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	if _, err := reader.ReadByte(); err == nil {
		if raw, err := conn.SyscallConn(); err == nil {
			_ = raw.Control(func(fd uintptr) {
				_ = syscall.SetsockoptLinger(int(fd), syscall.SOL_SOCKET, syscall.SO_LINGER, &syscall.Linger{Onoff: 1, Linger: 0})
			})
		}
	}
}

func (h *socketHarness) requestsSnapshot() []socketRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]socketRequest(nil), h.requests...)
}

func socketResponse(id json.RawMessage, result any) map[string]any {
	return map[string]any{"id": json.RawMessage(id), "result": result}
}

func testSocket(t *testing.T, h *socketHarness) *Socket {
	t.Helper()
	return NewSocket(SocketOptions{Path: h.path(), DialTimeout: time.Second, OpTimeout: time.Second})
}

func TestSocketRuntimeMethodsUseOneRequestPerConnectionAndExactParams(t *testing.T) {
	h := newSocketHarness(t, func(req socketRequest) any {
		switch req.Method {
		case "ping":
			return socketResponse(req.ID, map[string]any{"type": "pong", "version": "0.7.5", "protocol": 17})
		case "session.snapshot":
			return socketResponse(req.ID, map[string]any{"type": "snapshot", "snapshot": map[string]any{"panes": []any{map[string]any{"pane_id": "p1", "terminal_id": "term-1", "terminal_title": "Claude", "agent": "claude"}}}})
		case "pane.list":
			return socketResponse(req.ID, map[string]any{"type": "pane_list", "panes": []any{map[string]any{"pane_id": "p1", "terminal_id": "term-1", "terminal_title": "Claude", "agent": "claude"}}})
		case "pane.read":
			return socketResponse(req.ID, map[string]any{"type": "pane_read", "read": map[string]any{"text": "screen", "revision": 3}})
		case "pane.process_info":
			return socketResponse(req.ID, map[string]any{"type": "pane_process_info", "process_info": map[string]any{"foreground_processes": []any{map[string]any{"name": "claude", "cwd": "/work"}}}})
		case "pane.send_text", "pane.send_keys", "notification.show":
			return socketResponse(req.ID, map[string]any{"type": "ok"})
		default:
			t.Fatalf("unexpected method %q", req.Method)
			return nil
		}
	})
	client := testSocket(t, h)

	if got, err := client.SelfPaneID(); err != nil || got != "" {
		t.Fatalf("SelfPaneID() = %q, %v; socket transport must not read HERDR_*", got, err)
	}
	panes, err := client.ListPanes()
	if err != nil || !reflect.DeepEqual(panes, []runtimeapi.Pane{{ID: "p1", TerminalID: "term-1", Title: "Claude", Agent: "claude"}}) {
		t.Fatalf("ListPanes() = %#v, %v", panes, err)
	}
	if got, err := client.ReadPane("p1", 17); err != nil || got != "screen" {
		t.Fatalf("ReadPane() = %q, %v", got, err)
	}
	if got, err := client.ProcessInfo("p1"); err != nil || got != (runtimeapi.ProcessInfo{Command: "claude", CWD: "/work"}) {
		t.Fatalf("ProcessInfo() = %#v, %v", got, err)
	}
	if err := client.SendText("p1", "a; b"); err != nil {
		t.Fatalf("SendText(): %v", err)
	}
	if err := client.SendKeys("p1", runtimeapi.KeyEscape, runtimeapi.KeyEnter, "ctrl-c"); err != nil {
		t.Fatalf("SendKeys(): %v", err)
	}
	if err := client.Notify("title", "body"); err != nil {
		t.Fatalf("Notify(): %v", err)
	}
	pong, err := client.Ping()
	if err != nil || pong.Protocol != 17 || pong.Version != "0.7.5" {
		t.Fatalf("Ping() = %#v, %v", pong, err)
	}
	snapshot, err := client.Snapshot()
	if err != nil || len(snapshot.Panes) != 1 || snapshot.Panes[0].TerminalID != "term-1" {
		t.Fatalf("Snapshot() = %#v, %v", snapshot, err)
	}

	requests := h.requestsSnapshot()
	if len(requests) != 8 {
		t.Fatalf("request count = %d, want one fresh request for each call: %#v", len(requests), requests)
	}
	want := []struct {
		method string
		params map[string]any
	}{
		{"pane.list", map[string]any{}},
		{"pane.read", map[string]any{"pane_id": "p1", "source": "detection", "lines": float64(17), "strip_ansi": true}},
		{"pane.process_info", map[string]any{"pane_id": "p1"}},
		{"pane.send_text", map[string]any{"pane_id": "p1", "text": "a; b"}},
		{"pane.send_keys", map[string]any{"pane_id": "p1", "keys": []any{"esc", "enter", "ctrl-c"}}},
		{"notification.show", map[string]any{"title": "title", "body": "body"}},
		{"ping", map[string]any{}},
		{"session.snapshot", map[string]any{}},
	}
	for i, expected := range want {
		if requests[i].Method != expected.method || !reflect.DeepEqual(requests[i].Params, expected.params) {
			t.Errorf("request %d = %s %#v, want %s %#v", i, requests[i].Method, requests[i].Params, expected.method, expected.params)
		}
	}
}

func TestSocketIgnoresHerdrSocketEnvironment(t *testing.T) {
	h := newSocketHarness(t, func(req socketRequest) any {
		return socketResponse(req.ID, map[string]any{"type": "pong", "protocol": 17})
	})
	t.Setenv("HERDR_SOCKET_PATH", t.TempDir()+"/wrong.sock")
	client := NewSocket(SocketOptions{Path: h.path(), DialTimeout: time.Second, OpTimeout: time.Second})
	if _, err := client.Ping(); err != nil {
		t.Fatalf("Ping() used HERDR_SOCKET_PATH: %v", err)
	}
}

func TestSocketChecksResponseID(t *testing.T) {
	h := newSocketHarness(t, func(req socketRequest) any {
		return map[string]any{"id": "wrong", "result": map[string]any{"type": "pong", "protocol": 17}}
	})
	_, err := testSocket(t, h).Ping()
	if err == nil || !strings.Contains(err.Error(), "response id mismatch") {
		t.Fatalf("Ping() error = %v, want id mismatch", err)
	}
}

func TestSocketReturnsHerdrError(t *testing.T) {
	h := newSocketHarness(t, func(req socketRequest) any {
		return map[string]any{"id": json.RawMessage(req.ID), "error": map[string]any{"code": "missing", "message": "no pane"}}
	})
	_, err := testSocket(t, h).ReadPane("missing", 0)
	var herdrErr HerdrError
	if !errors.As(err, &herdrErr) || herdrErr.Code != "missing" || herdrErr.Message != "no pane" {
		t.Fatalf("error = %v, want HerdrError", err)
	}
}

func TestSocketDeadlineBoundsNoResponse(t *testing.T) {
	path := t.TempDir() + "/herdr.sock"
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			return
		}
		defer conn.Close()
		<-stop
	}()
	client := NewSocket(SocketOptions{Path: path, DialTimeout: time.Second, OpTimeout: 40 * time.Millisecond})
	start := time.Now()
	_, err = client.Ping()
	if err == nil || time.Since(start) > time.Second || !strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("Ping() error = %v after %s, want bounded timeout", err, time.Since(start))
	}
}

func TestSocketCloseBeforeResponseWrapsConnectionError(t *testing.T) {
	path := t.TempDir() + "/herdr.sock"
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.AcceptUnix()
		if err == nil {
			_, _ = bufio.NewReader(conn).ReadBytes('\n')
			_ = conn.Close()
		}
	}()
	_, err = NewSocket(SocketOptions{Path: path, DialTimeout: time.Second, OpTimeout: time.Second}).Ping()
	if err == nil || !strings.Contains(err.Error(), "read herdr response") {
		t.Fatalf("Ping() error = %v, want wrapped response read error", err)
	}
}

func TestSocketHarnessRejectsPipelinedRequestsWithReset(t *testing.T) {
	h := newSocketHarness(t, func(req socketRequest) any {
		return socketResponse(req.ID, map[string]any{"type": "pong", "protocol": 17})
	})
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: h.path(), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	first := []byte(`{"id":1,"method":"ping","params":{}}
`)
	second := []byte(`{"id":2,"method":"ping","params":{}}
`)
	if _, err := conn.Write(append(first, second...)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	if _, err := reader.ReadBytes('\n'); err != nil {
		t.Fatalf("first response: %v", err)
	}
	_, err = reader.ReadBytes('\n')
	if err == nil || (!errors.Is(err, syscall.ECONNRESET) && !strings.Contains(fmt.Sprint(err), "connection reset") && !errors.Is(err, io.EOF)) {
		t.Fatalf("second response error = %v, want reset/closed connection from one-request harness", err)
	}
}

func TestSocketDefaultPathDoesNotConsultHerdrEnvironment(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "/wrong.sock")
	path, err := defaultSocketPath()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := home + "/.config/herdr/herdr.sock"
	if path != want {
		t.Fatalf("defaultSocketPath() = %q, want %q", path, want)
	}
}
