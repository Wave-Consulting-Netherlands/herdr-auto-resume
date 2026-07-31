// Package terminal contains small, dependency-free terminal text helpers.
package terminal

import "strings"

// StripANSI removes terminal control sequences while retaining printable text,
// newlines, and tabs. It understands the sequence families emitted by modern
// terminal UIs, including OSC hyperlinks and private-mode CSI sequences.
func StripANSI(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			i = skipEscape(s, i)
			continue
		}
		if s[i] == '\r' {
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
				continue
			}
			i++
			continue
		}
		if s[i] == '\n' || s[i] == '\t' || s[i] >= 0x20 {
			out.WriteByte(s[i])
		}
		i++
	}
	return out.String()
}

func skipEscape(s string, i int) int {
	i++
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[':
		for j := i + 1; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
		return len(s)
	case 'P', '^', '_', 'X':
		return skipST(s, i+1)
	case ']':
		for j := i + 1; j < len(s); j++ {
			if s[j] == '\a' {
				return j + 1
			}
			if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	default:
		return i + 1
	}
}

func skipST(s string, i int) int {
	for j := i; j < len(s); j++ {
		if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
			return j + 2
		}
	}
	return len(s)
}

// Lines strips terminal controls and splits without dropping empty lines.
func Lines(s string) []string { return strings.Split(StripANSI(s), "\n") }

// Tail returns at most the last n lines. The returned slice aliases lines.
func Tail(lines []string, n int) []string {
	if n <= 0 {
		return nil
	}
	if n >= len(lines) {
		return lines
	}
	return lines[len(lines)-n:]
}

// TailString strips content and joins its last n lines.
func TailString(content string, n int) string { return strings.Join(Tail(Lines(content), n), "\n") }
