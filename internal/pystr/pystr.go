// Package pystr reproduces the Python str behaviour the Volt compiler's
// output depends on (splitlines, strip, split, slicing, isspace).
package pystr

import (
	"strings"
	"unicode"
)

// SplitLines matches Python's str.splitlines(): it breaks on every Unicode
// line boundary Python recognises, treats \r\n as one break, and drops a
// trailing empty line.
func SplitLines(s string) []string {
	var out []string
	start := 0
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			out = append(out, string(rs[start:i]))
			if rs[i] == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(rs) {
		out = append(out, string(rs[start:]))
	}
	return out
}

// IsSpace is Python's str.isspace() for one character: Unicode
// White_Space plus the \x1c-\x1f separators.
func IsSpace(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }

// Strip is str.strip() with no arguments.
func Strip(s string) string { return strings.TrimFunc(s, IsSpace) }

// Fields is str.split() with no arguments.
func Fields(s string) []string { return strings.FieldsFunc(s, IsSpace) }

// Slice is s[start:end] on code points with Python's negative-index and
// clamping rules.
func Slice(s string, start, end int) string {
	rs := []rune(s)
	n := len(rs)
	clamp := func(i int) int {
		if i < 0 {
			i += n
		}
		return max(0, min(i, n))
	}
	start, end = clamp(start), clamp(end)
	if start >= end {
		return ""
	}
	return string(rs[start:end])
}

// SliceFrom is s[start:].
func SliceFrom(s string, start int) string { return Slice(s, start, len(s)+1) }
