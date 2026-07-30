package runtime

import (
	"fmt"
	"strings"
)

type TextCall struct {
	PaneID string
	Text   string
}

type KeysCall struct {
	PaneID string
	Keys   []string
}

type Note struct {
	Title string
	Body  string
}

// Fake is an in-memory Runtime for coordinator and adapter-independent tests.
type Fake struct {
	PanesList []Pane
	Content   map[string]string
	Procs     map[string]ProcessInfo
	Self      string
	Errs      map[string]error

	SentText []TextCall
	SentKeys []KeysCall
	Notes    []Note
	Calls    []string
}

func (f *Fake) methodError(method string) error {
	if f.Errs == nil {
		return nil
	}
	return f.Errs[method]
}

func (f *Fake) record(method string, args ...string) {
	if len(args) == 0 {
		f.Calls = append(f.Calls, method)
		return
	}
	f.Calls = append(f.Calls, fmt.Sprintf("%s(%s)", method, strings.Join(args, ",")))
}

func (f *Fake) Name() string {
	f.record("Name")
	return "fake"
}

func (f *Fake) SelfPaneID() (string, error) {
	f.record("SelfPaneID")
	return f.Self, f.methodError("SelfPaneID")
}

func (f *Fake) ListPanes() ([]Pane, error) {
	f.record("ListPanes")
	panes := append([]Pane(nil), f.PanesList...)
	return panes, f.methodError("ListPanes")
}

func (f *Fake) ReadPane(paneID string, lines int) (string, error) {
	f.record("ReadPane", paneID)
	return f.Content[paneID], f.methodError("ReadPane")
}

func (f *Fake) ProcessInfo(paneID string) (ProcessInfo, error) {
	f.record("ProcessInfo", paneID)
	return f.Procs[paneID], f.methodError("ProcessInfo")
}

func (f *Fake) SendText(paneID, text string) error {
	f.record("SendText", paneID, text)
	f.SentText = append(f.SentText, TextCall{PaneID: paneID, Text: text})
	return f.methodError("SendText")
}

func (f *Fake) SendKeys(paneID string, keys ...string) error {
	f.record("SendKeys", paneID, strings.Join(keys, ","))
	f.SentKeys = append(f.SentKeys, KeysCall{PaneID: paneID, Keys: append([]string(nil), keys...)})
	return f.methodError("SendKeys")
}

func (f *Fake) Notify(title, body string) error {
	f.record("Notify", title, body)
	f.Notes = append(f.Notes, Note{Title: title, Body: body})
	return f.methodError("Notify")
}

var _ Runtime = (*Fake)(nil)
