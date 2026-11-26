package patch

import (
	"fmt"
	"sort"
	"strings"
)

// ApplyEdits applies SEARCH / REPLACE edits to LF-normalised text.
//
// Every SEARCH section is matched against the ORIGINAL text and must match
// exactly once (overlapping occurrences count as separate matches). Matched
// spans must not overlap each other. Nothing is applied unless every edit
// validates.
func ApplyEdits(path, text string, edits []Edit) (string, error) {
	type span struct {
		start, end int
		replace    string
	}
	spans := make([]span, 0, len(edits))
	for i, e := range edits {
		if e.Search == "" {
			return "", &Error{
				Title:  "Empty SEARCH section.",
				File:   path,
				Reason: fmt.Sprintf("block %d has an empty SEARCH section; creating or appending without context is not supported", i+1),
			}
		}
		n, first := countMatches(text, e.Search)
		if n == 0 {
			return "", &Error{
				Title:  "Patch context does not match.",
				File:   path,
				Reason: fmt.Sprintf("The SEARCH section of block %d was not found.", i+1),
				Causes: ContextCauses,
			}
		}
		if n > 1 {
			return "", &Error{
				Title:  "Patch context is ambiguous.",
				File:   path,
				Reason: fmt.Sprintf("The SEARCH section of block %d matches more than once. Add more surrounding lines to make it unique.", i+1),
			}
		}
		start := first
		end := first + len(e.Search)
		if e.DeleteLines && (start == 0 || text[start-1] == '\n') && end < len(text) && text[end] == '\n' {
			end++
		}
		spans = append(spans, span{start: start, end: end, replace: e.Replace})
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return "", &Error{
				Title:  "Conflicting edits.",
				File:   path,
				Reason: "Two SEARCH / REPLACE blocks overlap each other in the file.",
			}
		}
	}

	var b strings.Builder
	pos := 0
	for _, s := range spans {
		b.WriteString(text[pos:s.start])
		b.WriteString(s.replace)
		pos = s.end
	}
	b.WriteString(text[pos:])
	return b.String(), nil
}

// countMatches counts occurrences of sub in s, including overlapping ones.
// The count is capped at 2 because callers only need "none, one, or many".
// first is the index of the first occurrence, or -1.
func countMatches(s, sub string) (count, first int) {
	first = -1
	from := 0
	for from <= len(s) {
		i := strings.Index(s[from:], sub)
		if i < 0 {
			return count, first
		}
		if first < 0 {
			first = from + i
		}
		count++
		if count >= 2 {
			return count, first
		}
		from += i + 1
	}
	return count, first
}

// ApplyHunks applies unified diff hunks to LF-normalised text. It returns the
// new text and any warnings (for example, when a hunk was applied at a
// different line than its header states).
//
// A hunk is applied at its stated line when its old lines match there.
// Otherwise it is applied only if its old lines match at exactly one other
// position in the file. Zero or multiple candidates abort the whole patch.
func ApplyHunks(path, text string, hunks []Hunk) (string, []string, error) {
	lines, finalNL := splitLines(text)
	type placed struct {
		start  int
		oldLen int
		hunk   Hunk
	}
	var plan []placed
	var warnings []string
	prevEnd := 0

	for i, h := range hunks {
		old := oldSide(h)
		wanted := h.OldStart - 1
		if h.OldCount == 0 {
			wanted = h.OldStart
		}
		start, err := locate(lines, old, wanted)
		if err != nil {
			return "", nil, &Error{
				Title:  "Patch context does not match.",
				File:   path,
				Reason: fmt.Sprintf("Hunk %d (@@ -%d,%d @@): %s", i+1, h.OldStart, h.OldCount, err.Error()),
				Causes: ContextCauses,
			}
		}
		if start != wanted {
			warnings = append(warnings, fmt.Sprintf("%s: hunk %d applied at line %d instead of the stated line %d (the context matched exactly once).",
				path, i+1, start+1, wanted+1))
		}
		if start < prevEnd {
			return "", nil, &Error{
				Title:  "Conflicting hunks.",
				File:   path,
				Reason: fmt.Sprintf("Hunk %d overlaps or precedes the previous hunk.", i+1),
			}
		}
		prevEnd = start + len(old)
		plan = append(plan, placed{start: start, oldLen: len(old), hunk: h})
	}

	out := make([]string, 0, len(lines)+8)
	pos := 0
	for _, p := range plan {
		out = append(out, lines[pos:p.start]...)
		for _, hl := range p.hunk.Lines {
			if hl.Op == ' ' || hl.Op == '+' {
				out = append(out, hl.Text)
			}
		}
		pos = p.start + p.oldLen
	}
	out = append(out, lines[pos:]...)

	nl := finalNL
	if len(lines) == 0 {
		nl = true
	}
	return joinLines(out, nl), warnings, nil
}

func oldSide(h Hunk) []string {
	var old []string
	for _, l := range h.Lines {
		if l.Op == ' ' || l.Op == '-' {
			old = append(old, l.Text)
		}
	}
	return old
}

func matchesAt(lines, old []string, pos int) bool {
	if pos < 0 || pos+len(old) > len(lines) {
		return false
	}
	for i, o := range old {
		if lines[pos+i] != o {
			return false
		}
	}
	return true
}

func locate(lines, old []string, wanted int) (int, error) {
	if matchesAt(lines, old, wanted) {
		return wanted, nil
	}
	if len(old) == 0 {
		return 0, fmt.Errorf("insertion point is outside the file and the hunk has no context lines")
	}
	var found []int
	for pos := 0; pos+len(old) <= len(lines); pos++ {
		if matchesAt(lines, old, pos) {
			found = append(found, pos)
			if len(found) > 1 {
				break
			}
		}
	}
	switch len(found) {
	case 0:
		return 0, fmt.Errorf("the context and removed lines were not found in the file")
	case 1:
		return found[0], nil
	default:
		return 0, fmt.Errorf("the stated line does not match and the context appears in several places")
	}
}

// splitLines splits LF-normalised text into lines. "" is zero lines.
func splitLines(text string) (lines []string, finalNL bool) {
	if text == "" {
		return nil, false
	}
	finalNL = strings.HasSuffix(text, "\n")
	body := strings.TrimSuffix(text, "\n")
	return strings.Split(body, "\n"), finalNL
}

func joinLines(lines []string, finalNL bool) string {
	if len(lines) == 0 {
		return ""
	}
	s := strings.Join(lines, "\n")
	if finalNL {
		s += "\n"
	}
	return s
}

// SplitLines splits LF-normalised text into lines (exported for the diff
// package).
func SplitLines(text string) []string {
	l, _ := splitLines(text)
	return l
}
