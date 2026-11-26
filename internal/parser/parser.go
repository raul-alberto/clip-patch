// Package parser extracts patches from free-form text such as an AI assistant
// response copied from a chat window.
package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"clip-patch/internal/patch"
)

// Normalize strips a leading UTF-8 BOM and converts all line endings to LF.
func Normalize(s string) string {
	s = strings.TrimPrefix(s, "\uFEFF")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// Parse extracts a patch from text. With patch.FormatAuto the format is
// detected; if both formats appear, Parse refuses to guess.
func Parse(input string, format patch.Format) (*patch.Patch, error) {
	text := Normalize(input)
	if strings.TrimSpace(text) == "" {
		return nil, &patch.Error{Title: "Input is empty.", Reason: "There is no text to parse."}
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	switch format {
	case patch.FormatSearchReplace:
		return parseSearchReplace(lines)
	case patch.FormatUnifiedDiff:
		return parseUnifiedDiff(lines)
	case patch.FormatAuto, "":
		hasSR, hasUD := detect(lines)
		switch {
		case hasSR && hasUD:
			return nil, &patch.Error{
				Title:  "Ambiguous patch format.",
				Reason: "The text contains both SEARCH / REPLACE blocks and unified diff headers.",
				Causes: []string{"Re-run with --format search-replace or --format unified-diff to choose explicitly."},
			}
		case hasSR:
			return parseSearchReplace(lines)
		case hasUD:
			return parseUnifiedDiff(lines)
		}
		return nil, &patch.Error{
			Title:  "No supported patch found.",
			Reason: "Neither SEARCH / REPLACE blocks nor a unified diff were detected.",
			Causes: []string{
				"The copied text may be incomplete.",
				"Expected '<<<<<<< SEARCH' blocks under a '### FILE: path' header, or '--- a/file' / '+++ b/file' / '@@' diff sections.",
			},
		}
	}
	return nil, fmt.Errorf("unsupported format %q", format)
}

func detect(lines []string) (hasSR, hasUD bool) {
	for i, l := range lines {
		if isSearchMarker(l) {
			hasSR = true
		}
		if strings.HasPrefix(l, "diff --git ") {
			hasUD = true
		}
		if strings.HasPrefix(l, "--- ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ ") {
			hasUD = true
		}
	}
	return hasSR, hasUD
}

// ---------------------------------------------------------------------------
// SEARCH / REPLACE

var fileHeaderRe = regexp.MustCompile(`^\s*#{0,6}\s*FILE:\s*(.+?)\s*$`)

func isSearchMarker(l string) bool  { return strings.TrimRight(l, " \t") == "<<<<<<< SEARCH" }
func isDivider(l string) bool       { return strings.TrimRight(l, " \t") == "=======" }
func isReplaceMarker(l string) bool { return strings.TrimRight(l, " \t") == ">>>>>>> REPLACE" }

func cleanPathToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`\"'")
	return strings.ReplaceAll(s, "\\", "/")
}

func srError(reason string) error {
	return &patch.Error{Title: "Malformed SEARCH / REPLACE patch.", Reason: reason}
}

func parseSearchReplace(lines []string) (*patch.Patch, error) {
	const (
		stProse = iota
		stSearch
		stReplace
	)
	state := stProse
	curFile := ""
	blockStart := 0
	var search, replace []string

	var order []string
	byPath := map[string]*patch.FilePatch{}

	for i, raw := range lines {
		lineNo := i + 1
		switch state {
		case stProse:
			if m := fileHeaderRe.FindStringSubmatch(raw); m != nil {
				curFile = cleanPathToken(m[1])
				continue
			}
			if isSearchMarker(raw) {
				if curFile == "" {
					return nil, srError(fmt.Sprintf("line %d: SEARCH block appears before any '### FILE: path' header.", lineNo))
				}
				state = stSearch
				search = nil
				blockStart = lineNo
				continue
			}
			if isReplaceMarker(raw) {
				return nil, srError(fmt.Sprintf("line %d: '>>>>>>> REPLACE' without a matching SEARCH block.", lineNo))
			}
		case stSearch:
			switch {
			case isDivider(raw):
				state = stReplace
				replace = nil
			case isSearchMarker(raw):
				return nil, srError(fmt.Sprintf("line %d: new '<<<<<<< SEARCH' inside an unfinished SEARCH block (block started at line %d).", lineNo, blockStart))
			case isReplaceMarker(raw):
				return nil, srError(fmt.Sprintf("line %d: '>>>>>>> REPLACE' before the '=======' divider (block started at line %d).", lineNo, blockStart))
			default:
				search = append(search, raw)
			}
		case stReplace:
			switch {
			case isReplaceMarker(raw):
				e := patch.Edit{
					Search:      strings.Join(search, "\n"),
					Replace:     strings.Join(replace, "\n"),
					DeleteLines: len(replace) == 0,
				}
				if e.Search == "" {
					return nil, srError(fmt.Sprintf("block starting at line %d has an empty SEARCH section.", blockStart))
				}
				fp, ok := byPath[curFile]
				if !ok {
					fp = &patch.FilePatch{Path: curFile}
					byPath[curFile] = fp
					order = append(order, curFile)
				}
				fp.Edits = append(fp.Edits, e)
				state = stProse
			case isDivider(raw):
				return nil, srError(fmt.Sprintf("line %d: a second '=======' divider inside one block (block started at line %d). If the code itself contains such a line, the patch is ambiguous.", lineNo, blockStart))
			case isSearchMarker(raw):
				return nil, srError(fmt.Sprintf("line %d: '<<<<<<< SEARCH' inside an unfinished block (block started at line %d); the '>>>>>>> REPLACE' marker is missing.", lineNo, blockStart))
			default:
				replace = append(replace, raw)
			}
		}
	}
	if state != stProse {
		return nil, srError(fmt.Sprintf("the block starting at line %d is not terminated (the copied text may be truncated).", blockStart))
	}
	if len(order) == 0 {
		return nil, srError("no SEARCH / REPLACE blocks were found.")
	}

	p := &patch.Patch{Format: patch.FormatSearchReplace}
	for _, name := range order {
		p.Files = append(p.Files, *byPath[name])
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Unified diff

var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func udError(reason string) error {
	return &patch.Error{Title: "Malformed or unsupported unified diff.", Reason: reason}
}

func unsupported(file, what string) error {
	return &patch.Error{
		Title:  "Unsupported patch operation.",
		File:   file,
		Reason: what + ". clip-patch only modifies existing text files; no files were touched.",
	}
}

func parseUnifiedDiff(lines []string) (*patch.Patch, error) {
	var files []patch.FilePatch
	i := 0
	for i < len(lines) {
		l := lines[i]
		switch {
		case strings.HasPrefix(l, "diff --git "):
			fp, next, err := parseSection(lines, i)
			if err != nil {
				return nil, err
			}
			files = append(files, *fp)
			i = next
		case strings.HasPrefix(l, "--- ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ "):
			fp, next, err := parseSection(lines, i)
			if err != nil {
				return nil, err
			}
			files = append(files, *fp)
			i = next
		case strings.HasPrefix(l, "Binary files ") && strings.HasSuffix(l, " differ"):
			return nil, unsupported("", "binary file changes are not supported")
		default:
			i++
		}
	}
	if len(files) == 0 {
		return nil, udError("no file sections were found.")
	}
	return &patch.Patch{Format: patch.FormatUnifiedDiff, Files: files}, nil
}

// parseSection parses one file section starting at lines[i], which is either
// a "diff --git" line or a "--- " header line.
func parseSection(lines []string, i int) (*patch.FilePatch, int, error) {
	gitName := ""
	if strings.HasPrefix(lines[i], "diff --git ") {
		gitName = strings.TrimSpace(strings.TrimPrefix(lines[i], "diff --git "))
		i++
	header:
		for i < len(lines) {
			l := lines[i]
			switch {
			case strings.HasPrefix(l, "new file mode"):
				return nil, 0, unsupported(gitName, "creating new files is not supported")
			case strings.HasPrefix(l, "deleted file mode"):
				return nil, 0, unsupported(gitName, "deleting files is not supported")
			case strings.HasPrefix(l, "rename from"), strings.HasPrefix(l, "rename to"),
				strings.HasPrefix(l, "copy from"), strings.HasPrefix(l, "copy to"),
				strings.HasPrefix(l, "similarity index"), strings.HasPrefix(l, "dissimilarity index"):
				return nil, 0, unsupported(gitName, "renaming or copying files is not supported")
			case strings.HasPrefix(l, "old mode"), strings.HasPrefix(l, "new mode"):
				return nil, 0, unsupported(gitName, "file mode changes are not supported")
			case strings.HasPrefix(l, "Binary files "), strings.HasPrefix(l, "GIT binary patch"):
				return nil, 0, unsupported(gitName, "binary patches are not supported")
			case strings.HasPrefix(l, "index "):
				i++
			default:
				break header
			}
		}
	}

	if i+1 >= len(lines) || !strings.HasPrefix(lines[i], "--- ") || !strings.HasPrefix(lines[i+1], "+++ ") {
		name := gitName
		return nil, 0, &patch.Error{
			Title:  "Malformed or unsupported unified diff.",
			File:   name,
			Reason: "expected '--- ' and '+++ ' header lines (the diff may be truncated, or contains only unsupported changes).",
		}
	}
	oldP := headerPath(lines[i][4:])
	newP := headerPath(lines[i+1][4:])
	i += 2

	if oldP == "/dev/null" {
		return nil, 0, unsupported(newP, "creating new files is not supported")
	}
	if newP == "/dev/null" {
		return nil, 0, unsupported(oldP, "deleting files is not supported")
	}
	if strings.HasPrefix(oldP, "a/") && strings.HasPrefix(newP, "b/") {
		oldP = strings.TrimPrefix(oldP, "a/")
		newP = strings.TrimPrefix(newP, "b/")
	}
	if oldP != newP {
		return nil, 0, unsupported(newP, fmt.Sprintf("renaming files is not supported (%s -> %s)", oldP, newP))
	}

	fp := &patch.FilePatch{Path: newP}
	for i < len(lines) && strings.HasPrefix(lines[i], "@@") {
		h, next, err := parseHunk(lines, i, newP)
		if err != nil {
			return nil, 0, err
		}
		fp.Hunks = append(fp.Hunks, *h)
		i = next
	}
	if len(fp.Hunks) == 0 {
		return nil, 0, &patch.Error{
			Title:  "Malformed or unsupported unified diff.",
			File:   newP,
			Reason: "the file section contains no hunks.",
		}
	}
	if i < len(lines) && looksLikeDiffBody(lines, i) {
		return nil, 0, &patch.Error{
			Title:  "Malformed or unsupported unified diff.",
			File:   newP,
			Reason: fmt.Sprintf("line %d looks like diff content but is outside any hunk (the line counts in an @@ header may be too small).", i+1),
		}
	}
	return fp, i, nil
}

// looksLikeDiffBody reports whether lines[i] would be a hunk body line. It is
// used to detect leftover diff lines after a hunk's declared counts ran out,
// which must be an error rather than being silently ignored as prose.
func looksLikeDiffBody(lines []string, i int) bool {
	l := lines[i]
	if l == "" {
		return false
	}
	if strings.HasPrefix(l, "--- ") && i+1 < len(lines) && strings.HasPrefix(lines[i+1], "+++ ") {
		return false
	}
	switch l[0] {
	case ' ', '+', '-':
		return true
	}
	return false
}

func headerPath(s string) string {
	if idx := strings.IndexByte(s, '\t'); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s
}

func parseHunk(lines []string, i int, file string) (*patch.Hunk, int, error) {
	hdrLine := i + 1
	m := hunkRe.FindStringSubmatch(lines[i])
	if m == nil {
		return nil, 0, &patch.Error{
			Title:  "Malformed or unsupported unified diff.",
			File:   file,
			Reason: fmt.Sprintf("line %d: malformed hunk header %q.", hdrLine, lines[i]),
		}
	}
	num := func(s string, def int) int {
		if s == "" {
			return def
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return -1
		}
		return n
	}
	h := &patch.Hunk{
		OldStart: num(m[1], 0),
		OldCount: num(m[2], 1),
		NewStart: num(m[3], 0),
		NewCount: num(m[4], 1),
	}
	if h.OldStart < 0 || h.OldCount < 0 || h.NewStart < 0 || h.NewCount < 0 {
		return nil, 0, &patch.Error{
			Title:  "Malformed or unsupported unified diff.",
			File:   file,
			Reason: fmt.Sprintf("line %d: hunk header numbers are out of range.", hdrLine),
		}
	}
	i++
	remOld, remNew := h.OldCount, h.NewCount
	for remOld > 0 || remNew > 0 {
		if i >= len(lines) {
			return nil, 0, &patch.Error{
				Title:  "Malformed or unsupported unified diff.",
				File:   file,
				Reason: fmt.Sprintf("the hunk at line %d is truncated: it declares %d old / %d new lines but the text ended early.", hdrLine, h.OldCount, h.NewCount),
			}
		}
		l := lines[i]
		var op byte
		var text string
		switch {
		case l == "":
			op, text = ' ', ""
		case l[0] == ' ':
			op, text = ' ', l[1:]
		case l[0] == '-':
			op, text = '-', l[1:]
		case l[0] == '+':
			op, text = '+', l[1:]
		case l[0] == '\\':
			return nil, 0, unsupported(file, "'\\ No newline at end of file' markers are not supported")
		default:
			return nil, 0, &patch.Error{
				Title:  "Malformed or unsupported unified diff.",
				File:   file,
				Reason: fmt.Sprintf("line %d: unexpected line inside the hunk that starts at line %d (the line counts in the @@ header may be wrong).", i+1, hdrLine),
			}
		}
		bad := false
		switch op {
		case ' ':
			bad = remOld == 0 || remNew == 0
			remOld--
			remNew--
		case '-':
			bad = remOld == 0
			remOld--
		case '+':
			bad = remNew == 0
			remNew--
		}
		if bad {
			return nil, 0, &patch.Error{
				Title:  "Malformed or unsupported unified diff.",
				File:   file,
				Reason: fmt.Sprintf("line %d: the hunk that starts at line %d has more lines than its @@ header declares.", i+1, hdrLine),
			}
		}
		h.Lines = append(h.Lines, patch.HunkLine{Op: op, Text: text})
		i++
	}
	if i < len(lines) && strings.HasPrefix(lines[i], "\\") {
		return nil, 0, unsupported(file, "'\\ No newline at end of file' markers are not supported")
	}
	return h, i, nil
}
