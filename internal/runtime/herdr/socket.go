package herdr

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	runtimeapi "github.com/Wave-Consulting-Netherlands/herdr-auto-resume/internal/runtime"
)

const maxSocketFrame = 4 << 20

type SocketOptions struct {
	Path        string
	DialTimeout time.Duration
	OpTimeout   time.Duration
	Workspace   string
	ReadSource  string
}

type Socket struct {
	options  SocketOptions
	nextID   uint64
	eventsMu sync.Mutex
	events   *eventSession
}

type Pong struct {
	Type     string
	Version  string
	Protocol int
}

type Snapshot struct {
	Panes []runtimeapi.Pane
}

func NewSocket(options SocketOptions) *Socket {
	if options.Path == "" {
		options.Path, _ = defaultSocketPath()
	}
	if options.DialTimeout <= 0 {
		options.DialTimeout = 3 * time.Second
	}
	if options.OpTimeout <= 0 {
		options.OpTimeout = 5 * time.Second
	}
	if options.ReadSource == "" {
		options.ReadSource = "detection"
	}
	return &Socket{options: options}
}

func defaultSocketPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	if home == "" {
		return "", errors.New("home directory is empty")
	}
	return filepath.Join(home, ".config", "herdr", "herdr.sock"), nil
}

func (s *Socket) Name() string { return "herdr" }

// SelfPaneID intentionally returns no environment-derived identity. The socket
// transport has no self-pane operation, and inherited HERDR_* values are unsafe
// in a watcher launched from inside another Herdr pane.
func (s *Socket) SelfPaneID() (string, error) { return "", nil }

func (s *Socket) ListPanes() ([]runtimeapi.Pane, error) {
	params := map[string]any{}
	if s.options.Workspace != "" {
		params["workspace_id"] = s.options.Workspace
	}
	var result paneListResult
	if err := s.call("pane.list", params, &result); err != nil {
		return nil, err
	}
	return panesFromInfo(result.Panes), nil
}

func (s *Socket) ReadPane(paneID string, lines int) (string, error) {
	params := map[string]any{
		"pane_id":    paneID,
		"source":     s.options.ReadSource,
		"strip_ansi": true,
	}
	if lines > 0 {
		params["lines"] = lines
	}
	var result paneReadResult
	if err := s.call("pane.read", params, &result); err != nil {
		return "", err
	}
	return result.Read.Text, nil
}

func (s *Socket) ProcessInfo(paneID string) (runtimeapi.ProcessInfo, error) {
	var result processInfoResult
	if err := s.call("pane.process_info", map[string]any{"pane_id": paneID}, &result); err != nil {
		return runtimeapi.ProcessInfo{}, err
	}
	if len(result.ProcessInfo.ForegroundProcesses) == 0 {
		return runtimeapi.ProcessInfo{}, nil
	}
	process := result.ProcessInfo.ForegroundProcesses[0]
	if process.Name == "" {
		process.Name = process.Cmdline
	}
	return runtimeapi.ProcessInfo{Command: process.Name, CWD: process.CWD}, nil
}

func (s *Socket) SendText(paneID, text string) error {
	return s.call("pane.send_text", map[string]any{"pane_id": paneID, "text": text}, nil)
}

func (s *Socket) SendKeys(paneID string, keys ...string) error {
	translated := make([]string, len(keys))
	for i, key := range keys {
		translated[i] = herdrKey(key)
	}
	return s.call("pane.send_keys", map[string]any{"pane_id": paneID, "keys": translated}, nil)
}

func (s *Socket) Notify(title, body string) error {
	params := map[string]any{"title": title}
	if body != "" {
		params["body"] = body
	}
	return s.call("notification.show", params, nil)
}

func (s *Socket) Ping() (Pong, error) {
	var result pongResult
	if err := s.call("ping", map[string]any{}, &result); err != nil {
		return Pong{}, err
	}
	return Pong{Type: result.Type, Version: result.Version, Protocol: result.Protocol}, nil
}

func (s *Socket) Snapshot() (Snapshot, error) {
	var result snapshotResult
	if err := s.call("session.snapshot", map[string]any{}, &result); err != nil {
		return Snapshot{}, err
	}
	panes := result.Snapshot.Panes
	if panes == nil {
		panes = result.Panes
	}
	return Snapshot{Panes: panesFromInfo(panes)}, nil
}

func (s *Socket) call(method string, params any, target any) error {
	conn, err := net.DialTimeout("unix", s.options.Path, s.options.DialTimeout)
	if err != nil {
		return fmt.Errorf("dial herdr socket: %w", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(s.options.OpTimeout)); err != nil {
		return fmt.Errorf("set herdr socket deadline: %w", err)
	}

	id := atomic.AddUint64(&s.nextID, 1)
	request := struct {
		ID     uint64 `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{ID: id, Method: method, Params: params}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode herdr request: %w", err)
	}
	payload = append(payload, '\n')
	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("write herdr request: %w", err)
	}

	frame, err := readSocketFrame(bufio.NewReader(conn))
	if err != nil {
		return fmt.Errorf("read herdr response: %w", err)
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(frame, &envelope); err != nil {
		return fmt.Errorf("decode herdr response: %w", err)
	}
	if !responseIDMatches(envelope.ID, id) {
		return fmt.Errorf("herdr response id mismatch: got %s, want %d", string(envelope.ID), id)
	}
	if envelope.Error != nil {
		return *envelope.Error
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return errors.New("decode herdr response: missing result")
	}
	if target != nil {
		if err := json.Unmarshal(envelope.Result, target); err != nil {
			return fmt.Errorf("decode herdr result: %w", err)
		}
	}
	return nil
}

func readSocketFrame(reader *bufio.Reader) ([]byte, error) {
	frame, err := reader.ReadBytes('\n')
	if len(frame) > maxSocketFrame {
		return nil, fmt.Errorf("frame exceeds %d bytes", maxSocketFrame)
	}
	if err != nil {
		return nil, err
	}
	return frame[:len(frame)-1], nil
}

func responseIDMatches(raw json.RawMessage, want uint64) bool {
	var got uint64
	if err := json.Unmarshal(raw, &got); err == nil {
		return got == want
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text == fmt.Sprint(want)
	}
	return false
}

func panesFromInfo(infos []paneInfo) []runtimeapi.Pane {
	panes := make([]runtimeapi.Pane, 0, len(infos))
	for _, pane := range infos {
		panes = append(panes, runtimeapi.Pane{ID: pane.PaneID, TerminalID: pane.TerminalID, WorkspaceID: pane.WorkspaceID, Title: pane.TerminalTitle, Agent: pane.Agent})
	}
	return panes
}

func herdrKey(key string) string {
	switch key {
	case runtimeapi.KeyEscape:
		return "esc"
	case runtimeapi.KeyEnter:
		return "enter"
	default:
		return key
	}
}

var _ runtimeapi.Runtime = (*Socket)(nil)
