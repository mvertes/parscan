package scan

import (
	"fmt"
	"strings"
)

// Source describes a source text registered with a Sources table.
type Source struct {
	Name     string // "m:<label>" for inline, "f:<path>" for file
	Base     int    // base byte offset in the unified position space
	Len      int    // length in bytes
	contents string // source text for line/col resolution
}

// Sources is an ordered list of Source entries that maps global byte offsets
// to file/line/col triples. Multiple Compile calls (REPL, multi-file) each
// register their source via Add; Resolve translates any position back.
type Sources []Source

// Add registers a new source and returns its base offset. The name follows
// the convention "m:<label>" for inline / REPL input or "f:<path>" for files.
func (ss *Sources) Add(name, src string) int {
	base := 0
	if n := len(*ss); n > 0 {
		last := (*ss)[n-1]
		base = last.Base + last.Len + 1 // +1 for implicit newline separator
	}
	*ss = append(*ss, Source{
		Name:     name,
		Base:     base,
		Len:      len(src),
		contents: src,
	})
	return base
}

// Resolve converts a global byte offset to (source name, line, col).
// Returns ("", 0, 0) if pos is out of range.
func (ss Sources) Resolve(pos int) (name string, line, col int) {
	if len(ss) == 0 || pos < 0 {
		return "", 0, 0
	}
	// Linear scan backward (most recent source is the most likely match).
	i := len(ss) - 1
	for i > 0 && ss[i].Base > pos {
		i--
	}
	s := &ss[i]
	local := pos - s.Base
	if local < 0 || local > s.Len {
		return "", 0, 0
	}
	line, col = lineCol(s.contents, local)
	return s.Name, line, col
}

// FormatPos converts a global byte offset to a human-readable "file:line:col" string.
// For inline sources ("m:..."), the name is omitted and only "line:col" is returned.
func (ss Sources) FormatPos(pos int) string {
	name, line, col := ss.Resolve(pos)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "m:") {
		return fmt.Sprintf("%d:%d", line, col)
	}
	// Strip "f:" prefix for file sources.
	return fmt.Sprintf("%s:%d:%d", name[2:], line, col)
}

// lineCol computes 1-based line and column from a source string and byte offset.
func lineCol(src string, offset int) (line, col int) {
	if offset > len(src) {
		offset = len(src)
	}
	prefix := src[:offset]
	line = 1 + strings.Count(prefix, "\n")
	col = offset - strings.LastIndex(prefix, "\n")
	return line, col
}
