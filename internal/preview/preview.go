// Package preview renders a validated plan for human review.
package preview

import (
	"fmt"
	"io"
	"strings"

	"clip-patch/internal/apply"
	"clip-patch/internal/diff"
)

const (
	reset = "\x1b[0m"
	bold  = "\x1b[1m"
	red   = "\x1b[31m"
	green = "\x1b[32m"
	cyan  = "\x1b[36m"
	dim   = "\x1b[2m"
)

const rule = "----------------------------------------"

type checkedWriter struct {
	writer io.Writer
	err    error
}

func (w *checkedWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}

	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
	return n, err
}

func paint(on bool, code, s string) string {
	if !on {
		return s
	}
	return code + s + reset
}

// Render writes the preview for plan to w.
func Render(w io.Writer, version string, plan *apply.Plan, color bool) error {
	checked := &checkedWriter{writer: w}
	w = checked

	changed := 0
	edits := 0
	for _, c := range plan.Changes {
		if c.Changed() {
			changed++
		}
		edits += c.Edits
	}

	fmt.Fprintf(w, "%s\n\n", paint(color, bold, "clip-patch v"+version))
	fmt.Fprintf(w, "Patch detected: %s\n", plan.Format.Label())
	fmt.Fprintf(w, "Files affected: %d\n", changed)
	fmt.Fprintf(w, "Changes detected: %d\n\n", edits)

	for _, c := range plan.Changes {
		if c.Changed() {
			fmt.Fprintf(w, "[MODIFIED] %s\n", c.File.Rel)
		} else {
			fmt.Fprintf(w, "[UNCHANGED] %s (the patch produces identical content)\n", c.File.Rel)
		}
	}

	for _, c := range plan.Changes {
		if !c.Changed() {
			continue
		}
		fmt.Fprintf(w, "\n%s\n\n%s\n\n", rule, paint(color, bold, c.File.Rel))
		for _, warn := range c.Warnings {
			fmt.Fprintf(w, "NOTE: %s\n", warn)
		}
		if len(c.Warnings) > 0 {
			fmt.Fprintln(w)
		}
		text := diff.Unified(c.File.Rel, c.File.Text, c.NewText, 3)
		if text == "" {
			text = "(only line endings or the final newline differ)\n"
		}
		io.WriteString(w, colorize(text, color))
	}
	fmt.Fprintf(w, "\n%s\n\nPreview complete.\n", rule)
	return checked.err
}

func colorize(text string, color bool) string {
	if !color {
		return text
	}
	lines := strings.SplitAfter(text, "\n")
	var sb strings.Builder
	for _, l := range lines {
		if l == "" {
			continue
		}
		trimmed := strings.TrimRight(l, "\n")
		nl := l[len(trimmed):]
		switch {
		case strings.HasPrefix(trimmed, "+++ "), strings.HasPrefix(trimmed, "--- "):
			sb.WriteString(paint(true, bold, trimmed))
		case strings.HasPrefix(trimmed, "@@"):
			sb.WriteString(paint(true, cyan, trimmed))
		case strings.HasPrefix(trimmed, "+"):
			sb.WriteString(paint(true, green, trimmed))
		case strings.HasPrefix(trimmed, "-"):
			sb.WriteString(paint(true, red, trimmed))
		default:
			sb.WriteString(paint(true, dim, trimmed))
		}
		sb.WriteString(nl)
	}
	return sb.String()
}
