package terminal

import (
	"reflect"
	"testing"
)

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"csi", "\x1b[31mred\x1b[0m", "red"},
		{"private mode csi", "\x1b[?25lhidden\x1b[?25h", "hidden"},
		{"osc bel", "before\x1b]0;title\aafter", "beforeafter"},
		{"osc st", "before\x1b]8;;https://example.test\x1b\\link\x1b]8;;\x1b\\after", "beforelinkafter"},
		{"dcs", "before\x1bP1;2|payload\x1b\\after", "beforeafter"},
		{"apc", "before\x1b_payload\x1b\\after", "beforeafter"},
		{"c0 and carriage returns", "a\r\nb\rc\x00\x07\td", "a\nbc\td"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripANSI(tt.input); got != tt.want {
				t.Fatalf("StripANSI() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLinesPreservesEmptyPositions(t *testing.T) {
	got := Lines("a\n\x1b[31m\x1b[0m\n\nb")
	want := []string{"a", "", "", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Lines() = %#v, want %#v", got, want)
	}
}

func TestTailBounds(t *testing.T) {
	lines := []string{"a", "b", "c"}
	for _, tt := range []struct {
		n    int
		want []string
	}{
		{0, nil}, {-1, nil}, {2, []string{"b", "c"}}, {99, lines},
	} {
		if got := Tail(lines, tt.n); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Tail(%d) = %#v, want %#v", tt.n, got, tt.want)
		}
	}
	if got := TailString("a\nb\nc", 2); got != "b\nc" {
		t.Fatalf("TailString() = %q, want %q", got, "b\\nc")
	}
}
