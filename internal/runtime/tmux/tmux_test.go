package tmux

import (
	"reflect"
	"testing"
)

func TestParseListPanes(t *testing.T) {
	got, err := parseListPanes("%1 0 0 80 24 title with spaces\n%2 80 0 40 24 editor")
	if err != nil {
		t.Fatalf("parseListPanes returned error: %v", err)
	}
	if len(got.Panes) != 2 {
		t.Fatalf("parseListPanes returned %d panes, want 2", len(got.Panes))
	}
	if got.Panes[0].Title != "title with spaces" {
		t.Fatalf("first title = %q, want %q", got.Panes[0].Title, "title with spaces")
	}
	if got.Panes[1].Title != "editor" {
		t.Fatalf("second title = %q, want %q", got.Panes[1].Title, "editor")
	}
}

func TestParsePaneLineRejectsMalformedInput(t *testing.T) {
	tests := []string{
		"%1 0 0 80",
		"%1 left 0 80 24",
		"%1 0 top 80 24",
		"%1 0 0 width 24",
		"%1 0 0 80 height",
	}
	for _, line := range tests {
		t.Run(line, func(t *testing.T) {
			if _, err := parsePaneLine(line); err == nil {
				t.Fatalf("parsePaneLine(%q) returned nil error", line)
			}
		})
	}
}

func TestTranslateKeys(t *testing.T) {
	got := translateKeys("before", "escape", "enter", "literal", "Escape", "Enter")
	want := []string{"before", "Escape", "Enter", "literal", "Escape", "Enter"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("translateKeys = %#v, want %#v", got, want)
	}
}
