// Package patch defines the patch data model and the pure functions that
// apply a parsed patch to in-memory text. It performs no file or terminal I/O.
package patch

import (
	"fmt"
	"strings"
)

// Format identifies a supported patch format.
type Format string

// Supported formats.
const (
	FormatAuto          Format = "auto"
	FormatSearchReplace Format = "search-replace"
	FormatUnifiedDiff   Format = "unified-diff"
)

// Label returns a human readable name for the format.
func (f Format) Label() string {
	switch f {
	case FormatSearchReplace:
		return "SEARCH / REPLACE"
	case FormatUnifiedDiff:
		return "unified diff"
	default:
		return string(f)
	}
}

// ParseFormat converts a command-line value to a Format.
func ParseFormat(s string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(s))) {
	case FormatAuto:
		return FormatAuto, nil
	case FormatSearchReplace:
		return FormatSearchReplace, nil
	case FormatUnifiedDiff:
		return FormatUnifiedDiff, nil
	}
	return "", fmt.Errorf("unknown format %q (expected auto, search-replace or unified-diff)", s)
}

// Edit is one SEARCH / REPLACE block. Text is LF-normalised.
type Edit struct {
	Search  string
	Replace string
	// DeleteLines is true when the REPLACE section contained zero lines, in
	// which case the matched lines are removed entirely (including their
	// trailing newline) rather than leaving an empty line behind.
	DeleteLines bool
}

// HunkLine is one line of a unified diff hunk. Op is ' ', '-' or '+'.
type HunkLine struct {
	Op   byte
	Text string
}

// Hunk is one unified diff hunk.
type Hunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
	Lines              []HunkLine
}

// FilePatch holds all modifications for a single file. Exactly one of Edits
// and Hunks is used, depending on the patch format.
type FilePatch struct {
	Path  string
	Edits []Edit
	Hunks []Hunk
}

// Patch is a fully parsed patch.
type Patch struct {
	Format Format
	Files  []FilePatch
}

// ChangeCount returns the number of edits (SEARCH / REPLACE blocks) or hunks.
func (p *Patch) ChangeCount() int {
	n := 0
	for _, f := range p.Files {
		n += len(f.Edits) + len(f.Hunks)
	}
	return n
}

// Error is a user-facing error with enough structure to print a helpful
// message.
type Error struct {
	Title  string
	File   string
	Reason string
	Causes []string
}

func (e *Error) Error() string {
	s := strings.TrimSuffix(e.Title, ".")
	if e.File != "" {
		s += " [" + e.File + "]"
	}
	if e.Reason != "" {
		s += ": " + e.Reason
	}
	return s
}

// ContextCauses lists the usual reasons a patch does not match a file.
var ContextCauses = []string{
	"The file has changed since the AI generated the patch.",
	"The wrong project directory is selected (see --root).",
	"The patch refers to a different version of the file.",
}
